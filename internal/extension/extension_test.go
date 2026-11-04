package extension_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	beacon "github.com/dash1101/ytm-beacon"
	"github.com/dash1101/ytm-beacon/internal/extension"
)

func src(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(beacon.Extension, "extension")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestManifestVersion(t *testing.T) {
	for in, want := range map[string]string{"v1.2.3": "1.2.3", "1.2.3-rc1": "1.2.3", "dev": "0.0.0", "": "0.0.0", "v10.0.1+abc": "10.0.1"} {
		if got := extension.ManifestVersion(in); got != want {
			t.Errorf("ManifestVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// Every file a manifest or HTML page points at must really be in the package,
// otherwise the browser rejects the extension (or a button silently breaks).
func TestPackagesAreComplete(t *testing.T) {
	for _, browser := range extension.Browsers {
		t.Run(browser, func(t *testing.T) {
			var buf bytes.Buffer
			if err := extension.Zip(src(t), browser, "v1.2.3", &buf); err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{}
			for _, f := range zr.File {
				rc, _ := f.Open()
				b, _ := io.ReadAll(rc)
				rc.Close()
				files[f.Name] = b
			}

			raw, ok := files["manifest.json"]
			if !ok {
				t.Fatal("no manifest.json at the root of the package")
			}
			var m struct {
				ManifestVersion int               `json:"manifest_version"`
				Version         string            `json:"version"`
				Icons           map[string]string `json:"icons"`
				Action          struct {
					Popup string `json:"default_popup"`
				} `json:"action"`
				Background struct {
					ServiceWorker string   `json:"service_worker"`
					Scripts       []string `json:"scripts"`
				} `json:"background"`
				ContentScripts []struct {
					JS      []string `json:"js"`
					Matches []string `json:"matches"`
				} `json:"content_scripts"`
				Permissions     []string `json:"permissions"`
				HostPermissions []string `json:"host_permissions"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatalf("manifest.json is not valid JSON: %v", err)
			}
			if m.ManifestVersion != 3 || m.Version != "1.2.3" {
				t.Errorf("manifest_version=%d version=%q", m.ManifestVersion, m.Version)
			}
			// Nothing beyond the local app, the one site we read, and the API that
			// starts our script in tabs that were already open.
			if len(m.HostPermissions) != 2 || m.HostPermissions[0] != "http://127.0.0.1/*" || m.HostPermissions[1] != "https://music.youtube.com/*" {
				t.Errorf("host permissions = %v (should only be the local app and music.youtube.com)", m.HostPermissions)
			}
			if len(m.Permissions) != 2 || m.Permissions[0] != "scripting" || m.Permissions[1] != "storage" {
				t.Errorf("permissions = %v (should only be scripting and storage)", m.Permissions)
			}

			need := []string{m.Action.Popup}
			for _, p := range m.Icons {
				need = append(need, p)
			}
			for _, cs := range m.ContentScripts {
				need = append(need, cs.JS...)
				if len(cs.Matches) != 1 || cs.Matches[0] != "https://music.youtube.com/*" {
					t.Errorf("content script matches = %v", cs.Matches)
				}
			}
			switch browser {
			case "chrome":
				need = append(need, m.Background.ServiceWorker)
				if len(m.Background.Scripts) != 0 {
					t.Error("chrome manifest must not use background.scripts")
				}
			case "firefox":
				need = append(need, m.Background.Scripts...)
				if m.Background.ServiceWorker != "" {
					t.Error("firefox manifest must not use background.service_worker")
				}
			}
			// What the popup page loads.
			ref := regexp.MustCompile(`(?:src|href)="([^"#:]+)"`)
			for _, mt := range ref.FindAllSubmatch(files[m.Action.Popup], -1) {
				need = append(need, string(mt[1]))
			}

			for _, name := range need {
				if name == "" {
					t.Error("manifest has an empty file reference")
					continue
				}
				if _, ok := files[name]; !ok {
					t.Errorf("manifest or popup refers to %q, which is not in the package", name)
				}
			}
			for name := range files {
				if name != "manifest.json" && regexp.MustCompile(`^manifest\..+\.json$`).MatchString(name) {
					t.Errorf("the other browser's manifest leaked into the package: %s", name)
				}
			}
		})
	}
}

func TestWriteReplacesOwnFolderOnly(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "chrome")
	if err := extension.Write(src(t), "chrome", "1.0.0", dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "stale.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extension.Write(src(t), "chrome", "1.0.1", dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.js")); err == nil {
		t.Error("stale files from an older version must be removed")
	}
	b, _ := os.ReadFile(filepath.Join(dest, "manifest.json"))
	if !bytes.Contains(b, []byte(`"1.0.1"`)) {
		t.Error("manifest not updated")
	}

	// A folder that is not ours must never be wiped.
	other := t.TempDir()
	keep := filepath.Join(other, "important.txt")
	_ = os.WriteFile(keep, []byte("mine"), 0o644)
	if err := extension.Write(src(t), "chrome", "1.0.0", other); err == nil {
		t.Error("must refuse to overwrite an unrelated non-empty folder")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("unrelated file was deleted")
	}
}

func TestUnknownBrowser(t *testing.T) {
	if err := extension.Zip(src(t), "netscape", "1.0.0", io.Discard); err == nil {
		t.Error("unknown browser must be an error")
	}
}
