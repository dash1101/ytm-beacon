package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dash1101/ytm-beacon/internal/version"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "v0.1.0-dev", true}, // a release replaces its own dev build
		{"v0.1.0-rc1", "v0.1.0", false},
		{"v0.1.0", "dev", true},
		{"v0.1.0", "", true},
		{"garbage", "v0.1.0", false},
		{"v10.0.0", "v9.0.0", true}, // numeric, not text, comparison
	} {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

// release serves a fake release: an archive holding a stub "program" that just
// prints its arguments, plus the checksum list.
func release(t *testing.T, tamper bool) (*httptest.Server, *int32) {
	t.Helper()
	script := "#!/bin/sh\necho \"stub installer ran with: $@\"\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"./ytm-beacon": script, "./README.md": "hi", "../evil": "no"} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	if tamper {
		sum[0] ^= 0xff
	}
	name := assetName()
	var hits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&hits, 1); _, _ = w.Write(buf.Bytes()) })
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n%s  other_file.zip\n", hex.EncodeToString(sum[:]), name, strings.Repeat("0", 64))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, &hits
}

func TestUpdateDownloadsVerifiesAndRunsInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub installer is a shell script")
	}
	ts, hits := release(t, false)
	t.Setenv("YTM_BEACON_RELEASE_BASE", ts.URL)
	t.Setenv("YTM_BEACON_LATEST_TAG", "v9.9.9")

	var out bytes.Buffer
	if err := Run(&out, Options{}); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	for _, want := range []string{"Latest version: v9.9.9", "Checksum OK.", "stub installer ran with: install --no-open"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output is missing %q:\n%s", want, out.String())
		}
	}
	if *hits != 1 {
		t.Errorf("archive downloaded %d times", *hits)
	}
}

func TestUpdateRefusesATamperedDownload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub installer is a shell script")
	}
	ts, _ := release(t, true)
	t.Setenv("YTM_BEACON_RELEASE_BASE", ts.URL)
	t.Setenv("YTM_BEACON_LATEST_TAG", "v9.9.9")

	var out bytes.Buffer
	err := Run(&out, Options{})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want a checksum error, got %v", err)
	}
	if strings.Contains(out.String(), "stub installer ran") {
		t.Error("a tampered download must never be run")
	}
}

func TestUpdateWhenAlreadyCurrent(t *testing.T) {
	ts, hits := release(t, false)
	t.Setenv("YTM_BEACON_RELEASE_BASE", ts.URL)
	t.Setenv("YTM_BEACON_LATEST_TAG", "v0.0.1")
	prev := version.Version
	version.Version = "v0.0.1" // "dev" builds always count as updatable, so pin a real version
	t.Cleanup(func() { version.Version = prev })

	var out bytes.Buffer
	if err := Run(&out, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already have the latest") || *hits != 0 {
		t.Errorf("nothing should be downloaded when up to date (hits=%d):\n%s", *hits, out.String())
	}
}

func TestCheckOnlyDoesNotDownload(t *testing.T) {
	ts, hits := release(t, false)
	t.Setenv("YTM_BEACON_RELEASE_BASE", ts.URL)
	t.Setenv("YTM_BEACON_LATEST_TAG", "v9.9.9")
	var out bytes.Buffer
	if err := Run(&out, Options{Check: true}); err != nil {
		t.Fatal(err)
	}
	if *hits != 0 || !strings.Contains(out.String(), "Run `ytm-beacon update`") {
		t.Errorf("--check must only report (hits=%d):\n%s", *hits, out.String())
	}
}

func TestMissingChecksumEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unused on windows")
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			fmt.Fprint(w, strings.Repeat("0", 64)+"  something_else.tar.gz\n")
			return
		}
		_, _ = w.Write([]byte("not really an archive"))
	}))
	defer ts.Close()
	t.Setenv("YTM_BEACON_RELEASE_BASE", ts.URL)
	t.Setenv("YTM_BEACON_LATEST_TAG", "v9.9.9")
	err := Run(&bytes.Buffer{}, Options{})
	if err == nil || !strings.Contains(err.Error(), "no checksum is published") {
		t.Fatalf("got %v", err)
	}
}

// Downloads come from the release that was found, so a release published a
// second later can't hand over another version's files.
func TestDownloadsComeFromTheFoundTag(t *testing.T) {
	t.Setenv("YTM_BEACON_RELEASE_BASE", "")
	got := releaseBase("v0.1.5")
	if want := version.RepoURL + "/releases/download/v0.1.5"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if strings.Contains(releaseBase("v1/../../x"), "/../") {
		t.Error("a strange tag must not change the path")
	}
}
