// Package updater implements `ytm-beacon update`: find the latest release,
// download it, check its SHA-256 checksum, and run its installer. It only talks
// to the network when the user runs that command; the app itself never does.
package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dash1101/ytm-beacon/internal/version"
)

// Options tune `update`.
type Options struct {
	Check bool // only report whether a newer version exists
	Force bool // reinstall even if already up to date
}

// Developers can point the updater at another place (tests do).
//   - YTM_BEACON_RELEASE_BASE: folder URL that holds the release assets
//   - YTM_BEACON_LATEST_TAG:   skip asking GitHub which tag is the latest
//
// The files come from the release of the tag that was found, not from
// "latest", so a release published in between can't mix two versions.
func releaseBase(tag string) string {
	if v := os.Getenv("YTM_BEACON_RELEASE_BASE"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return version.RepoURL + "/releases/download/" + url.PathEscape(tag)
}

func assetName() string {
	name := fmt.Sprintf("ytm-beacon_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		return name + ".zip"
	}
	return name + ".tar.gz"
}

// latestTag asks GitHub which release is the latest, without using its API
// (so there are no rate limits): /releases/latest redirects to /releases/tag/<tag>.
func latestTag() (string, error) {
	if v := os.Getenv("YTM_BEACON_LATEST_TAG"); v != "" {
		return v, nil
	}
	client := &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Head(version.RepoURL + "/releases/latest")
	if err != nil {
		return "", offline("check for a new version", err)
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode/100 != 3 || !strings.Contains(loc, "/tag/") {
		return "", fmt.Errorf("no published release found at %s/releases (is the project public and has a release been created?)", version.RepoURL)
	}
	return loc[strings.LastIndex(loc, "/tag/")+len("/tag/"):], nil
}

type semver struct {
	major, minor, patch int
	pre                 bool
	ok                  bool
}

var semverPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?`)

func parse(v string) semver {
	m := semverPattern.FindStringSubmatch(v)
	if m == nil {
		return semver{}
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	return semver{a, b, c, m[4] != "", true}
}

// Newer reports whether latest is a newer version than current. Builds that
// aren't a release (like "dev") can always be replaced by a release.
func Newer(latest, current string) bool {
	l, c := parse(latest), parse(current)
	if !l.ok {
		return false
	}
	if !c.ok {
		return true
	}
	switch {
	case l.major != c.major:
		return l.major > c.major
	case l.minor != c.minor:
		return l.minor > c.minor
	case l.patch != c.patch:
		return l.patch > c.patch
	}
	return c.pre && !l.pre // 1.2.3-dev is older than 1.2.3
}

// Run updates the installed program.
func Run(out io.Writer, opt Options) error {
	tag, err := latestTag()
	if err != nil {
		return err
	}
	cur := version.Version
	if !opt.Force && !Newer(tag, cur) {
		fmt.Fprintf(out, "You already have the latest version (%s).\n", cur)
		return nil
	}
	fmt.Fprintf(out, "Latest version: %s (you have %s)\n", tag, cur)
	if opt.Check {
		fmt.Fprintln(out, "Run `ytm-beacon update` to install it.")
		return nil
	}

	tmp, err := os.MkdirTemp("", "ytm-beacon-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	asset := assetName()
	base := releaseBase(tag)
	archive := filepath.Join(tmp, asset)
	sums := filepath.Join(tmp, "checksums.txt")
	fmt.Fprintf(out, "Downloading %s ...\n", asset)
	if err := download(base+"/"+asset, archive); err != nil {
		return err
	}
	if err := download(base+"/checksums.txt", sums); err != nil {
		return err
	}
	if err := verify(archive, asset, sums); err != nil {
		return err
	}
	fmt.Fprintln(out, "Checksum OK.")

	exe, err := extract(archive, filepath.Join(tmp, "new"))
	if err != nil {
		return err
	}
	// The new program installs itself: it stops the old copy, replaces it,
	// and starts again. --no-open: this is an update, not a first setup.
	cmd := exec.Command(exe, "install", "--no-open")
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// offline explains a network failure in plain words, keeping the technical reason on its own line for bug reports.
func offline(what string, err error) error {
	reason := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) {
		reason = ue.Err.Error() // drop the repeated method and address
	}
	return fmt.Errorf("couldn't reach GitHub to %s. Check your internet connection and try again.\n  (%s)", what, reason)
}

func download(url, dst string) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return offline("download "+path.Base(url), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s answered %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// verify checks the archive against the line for it in checksums.txt
// ("<sha256>  <file name>").
func verify(archive, name, sumsFile string) error {
	b, err := os.ReadFile(sumsFile)
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			want = strings.ToLower(f[0])
		}
	}
	if want == "" {
		return fmt.Errorf("checksum check failed: no checksum is published for %s", name)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.New("checksum check FAILED: the download does not match its published checksum, so it was not installed")
	}
	return nil
}

// wanted are the only files taken out of an archive, by plain file name, so a
// crafted archive can't write anywhere else.
var wanted = map[string]bool{"ytm-beacon": true, "ytm-beacon.exe": true, "ytm-beacon-bg.exe": true}

// extract unpacks the program(s) into dst and returns the path of the main one.
func extract(archive, dst string) (string, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	write := func(name string, r io.Reader) error {
		f, err := os.OpenFile(filepath.Join(dst, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, r); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}

	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return "", err
		}
		defer zr.Close()
		for _, zf := range zr.File {
			if name := path.Base(zf.Name); wanted[name] && !zf.FileInfo().IsDir() {
				rc, err := zf.Open()
				if err != nil {
					return "", err
				}
				err = write(name, rc)
				rc.Close()
				if err != nil {
					return "", err
				}
			}
		}
	} else {
		f, err := os.Open(archive)
		if err != nil {
			return "", err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if name := path.Base(h.Name); wanted[name] && h.Typeflag == tar.TypeReg {
				if err := write(name, tr); err != nil {
					return "", err
				}
			}
		}
	}

	exe := filepath.Join(dst, "ytm-beacon")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if _, err := os.Stat(exe); err != nil {
		return "", errors.New("the download did not contain the program")
	}
	return exe, nil
}
