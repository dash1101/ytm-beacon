// Package extension unpacks the browser extension that is embedded in the app,
// either into a folder (to load into a browser) or into a .zip (for releases).
package extension

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Browsers are the extension flavours: Chrome, Brave, Edge, Opera... all take
// "chrome"; Firefox takes "firefox".
var Browsers = []string{"chrome", "firefox"}

// Placeholders in the extension sources that are filled in when packaging.
const (
	versionPlaceholder = "__VERSION__" // in manifests
	buildPlaceholder   = "__BUILD__"   // in .js files
)

var numericVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// ManifestVersion turns an app version like "v1.2.3-rc1" or "dev" into the
// plain dotted number browsers require.
func ManifestVersion(v string) string {
	if m := numericVersion.FindStringSubmatch(v); m != nil {
		return m[1] + "." + m[2] + "." + m[3]
	}
	return "0.0.0"
}

func isManifest(name string) bool {
	return strings.HasPrefix(name, "manifest.") && strings.HasSuffix(name, ".json")
}

func sourceFiles(src fs.FS) ([]string, error) {
	var names []string
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			names = append(names, p)
		}
		return nil
	})
	sort.Strings(names)
	return names, err
}

// Build identifies this exact version of the extension code (the same for every
// browser flavour). The app tells a running extension its build; when the two
// differ, the extension has been left behind by an update and reloads itself.
func Build(src fs.FS, version string) string {
	names, err := sourceFiles(src)
	if err != nil {
		return ""
	}
	h := sha256.New()
	for _, name := range names {
		data, err := fs.ReadFile(src, name)
		if err != nil {
			return ""
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	h.Write([]byte(ManifestVersion(version)))
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// walk calls fn for every file of the extension for one browser, with the
// right manifest installed as manifest.json and the placeholders filled in.
func walk(src fs.FS, browser, version string, fn func(name string, data []byte) error) error {
	known := false
	for _, b := range Browsers {
		known = known || b == browser
	}
	if !known {
		return fmt.Errorf("unknown browser %q (use chrome or firefox)", browser)
	}
	names, err := sourceFiles(src)
	if err != nil {
		return err
	}
	build := Build(src, version)

	sawManifest := false
	for _, name := range names {
		data, err := fs.ReadFile(src, name)
		if err != nil {
			return err
		}
		out := name
		switch {
		case isManifest(path.Base(name)):
			if path.Base(name) != "manifest."+browser+".json" {
				continue // another browser's manifest
			}
			out = "manifest.json"
			data = bytes.ReplaceAll(data, []byte(versionPlaceholder), []byte(ManifestVersion(version)))
			sawManifest = true
		case strings.HasSuffix(name, ".js"):
			data = bytes.ReplaceAll(data, []byte(buildPlaceholder), []byte(build))
		}
		if err := fn(out, data); err != nil {
			return err
		}
	}
	if !sawManifest {
		return fmt.Errorf("the extension has no manifest for %s", browser)
	}
	return nil
}

// Write unpacks the extension into dest, replacing what was there. It refuses
// to touch a non-empty folder that doesn't look like a previous copy.
func Write(src fs.FS, browser, version, dest string) error {
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(dest, "manifest.json")); err != nil {
			return fmt.Errorf("%s is not empty and is not a YTM Beacon extension folder; not overwriting it", dest)
		}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return walk(src, browser, version, func(name string, data []byte) error {
		p := filepath.Join(dest, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, data, 0o644)
	})
}

// Zip writes the extension for one browser as a .zip (manifest.json at the root).
func Zip(src fs.FS, browser, version string, w io.Writer) error {
	zw := zip.NewWriter(w)
	err := walk(src, browser, version, func(name string, data []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o644)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}
