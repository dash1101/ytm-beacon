package extension_test

import (
	"archive/zip"
	"bytes"
	"io"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dash1101/ytm-beacon/internal/extension"
)

func sampleFS(background string) fstest.MapFS {
	return fstest.MapFS{
		"manifest.chrome.json":  {Data: []byte(`{"version":"__VERSION__"}`)},
		"manifest.firefox.json": {Data: []byte(`{"version":"__VERSION__","gecko":true}`)},
		"background.js":         {Data: []byte(background)},
		"content.js":            {Data: []byte(`console.log('hi')`)},
	}
}

func unzip(t *testing.T, src fs.FS, browser, version string) map[string]string {
	t.Helper()
	var buf bytes.Buffer
	if err := extension.Zip(src, browser, version, &buf); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestBuildIsStableAndChangesWithTheCode(t *testing.T) {
	a := extension.Build(sampleFS(`const BUILD = '__BUILD__';`), "v1.2.3")
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(a) {
		t.Fatalf("build id %q is not 12 hex characters", a)
	}
	if b := extension.Build(sampleFS(`const BUILD = '__BUILD__';`), "v1.2.3"); a != b {
		t.Error("the same sources must give the same build id")
	}
	if b := extension.Build(sampleFS(`const BUILD = '__BUILD__'; // changed`), "v1.2.3"); a == b {
		t.Error("changed code must change the build id, or extensions would never reload")
	}
	if b := extension.Build(sampleFS(`const BUILD = '__BUILD__';`), "v1.2.4"); a == b {
		t.Error("a new version must change the build id")
	}
	// Same id whichever flavour is asking: the app only has one answer for both.
	one := unzip(t, sampleFS(`const BUILD = '__BUILD__';`), "chrome", "v1.2.3")["background.js"]
	two := unzip(t, sampleFS(`const BUILD = '__BUILD__';`), "firefox", "v1.2.3")["background.js"]
	if one != two || !strings.Contains(one, a) {
		t.Errorf("both flavours must carry build %s: %q vs %q", a, one, two)
	}
}

func TestPlaceholdersAreFilledIn(t *testing.T) {
	files := unzip(t, sampleFS(`const BUILD = '__BUILD__';`), "chrome", "v1.2.3")
	if strings.Contains(files["background.js"], "__BUILD__") || strings.Contains(files["manifest.json"], "__VERSION__") {
		t.Errorf("placeholders left behind: %v", files)
	}
	if !strings.Contains(files["manifest.json"], `"1.2.3"`) {
		t.Errorf("manifest version: %s", files["manifest.json"])
	}
}

// The real extension must carry the build id the app will announce, otherwise
// every extension would reload itself once, and a mismatch means it never settles.
func TestRealExtensionCarriesTheAppsBuild(t *testing.T) {
	real := src(t)
	want := extension.Build(real, "v0.4.2")
	for _, browser := range extension.Browsers {
		bg := unzip(t, real, browser, "v0.4.2")["background.js"]
		if strings.Contains(bg, "__BUILD__") {
			t.Errorf("%s: background.js still has the placeholder", browser)
		}
		if !strings.Contains(bg, "'"+want+"'") {
			t.Errorf("%s: background.js does not contain build %s", browser, want)
		}
	}
}
