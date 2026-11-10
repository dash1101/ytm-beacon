package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
	"github.com/dash1101/ytm-beacon/internal/presence"
)

func newTestServer(t *testing.T) (*httptest.Server, *config.Store) {
	t.Helper()
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	cfg := config.NewMemoryStore(config.Default())
	tr := presence.NewTracker()
	eng := presence.NewEngine(cfg, tr, func(context.Context, string) (discord.Conn, error) {
		return nil, errors.New("no discord in tests")
	})
	srv := New(Deps{Config: cfg, Tracker: tr, Engine: eng}, 41713)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, cfg
}

func do(t *testing.T, ts *httptest.Server, method, path, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

const goodUpdate = `{"source":"tab1","agent":"Brave","state":"playing","title":"Song","artist":"Artist","album":"Album","videoId":"dQw4w9WgXcQ","position":12.5,"duration":200}`

func TestUpdateFromExtension(t *testing.T) {
	ts, _ := newTestServer(t)

	// The extension sends text/plain with a chrome-extension origin.
	resp := do(t, ts, "POST", "/v1/update", goodUpdate, map[string]string{
		"Content-Type": "text/plain", "Origin": "chrome-extension://abcdefghijklmnop",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "chrome-extension://abcdefghijklmnop" {
		t.Errorf("CORS header = %q", got)
	}

	var st Status
	resp = do(t, ts, "GET", "/v1/status", "", nil)
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.App != "ytm-beacon" || st.NowPlaying == nil || st.NowPlaying.Title != "Song" {
		t.Errorf("status does not show the track: %+v", st.NowPlaying)
	}
	if st.Extension.LastSeenAgoSec == nil || len(st.Extension.Sources) != 1 || st.Extension.Sources[0].Agent != "Brave" {
		t.Errorf("extension status wrong: %+v", st.Extension)
	}
	if st.Discord.State != presence.StateConnecting {
		t.Logf("discord state: %s", st.Discord.State) // the engine loop is not running in this test
	}
}

func TestFirefoxOriginAndPreflight(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := do(t, ts, "OPTIONS", "/v1/update", "", map[string]string{"Origin": "moz-extension://1234-5678"})
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "moz-extension://1234-5678" {
		t.Errorf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
}

func TestRejectsWebsitesAndForeignHosts(t *testing.T) {
	ts, _ := newTestServer(t)

	resp := do(t, ts, "POST", "/v1/update", goodUpdate, map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a website must not be able to post updates, got %d", resp.StatusCode)
	}
	resp = do(t, ts, "GET", "/v1/status", "", map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a website must not read the status, got %d", resp.StatusCode)
	}
	resp = do(t, ts, "POST", "/v1/quit", "{}", map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a website must not be able to stop the app, got %d", resp.StatusCode)
	}
	resp = do(t, ts, "GET", "/v1/status", "", map[string]string{"Host": "evil.example:41713"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("DNS rebinding must be refused, got %d", resp.StatusCode)
	}
}

func TestDashboardIsSameOrigin(t *testing.T) {
	ts, _ := newTestServer(t)
	host := strings.TrimPrefix(ts.URL, "http://")
	resp := do(t, ts, "GET", "/", "", nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("dashboard: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp = do(t, ts, "POST", "/v1/test", "", map[string]string{"Origin": "http://" + host})
	if resp.StatusCode != 200 {
		t.Errorf("the dashboard's own requests must work, got %d", resp.StatusCode)
	}
}

func TestBadUpdates(t *testing.T) {
	ts, _ := newTestServer(t)
	for name, body := range map[string]string{
		"no title":        `{"source":"a"}`,
		"no source":       `{"title":"x"}`,
		"not json":        `hello`,
		"reserved source": `{"source":"test","title":"x"}`,
	} {
		if resp := do(t, ts, "POST", "/v1/update", body, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, resp.StatusCode)
		}
	}
}

func TestClear(t *testing.T) {
	ts, _ := newTestServer(t)
	do(t, ts, "POST", "/v1/update", goodUpdate, nil)
	do(t, ts, "POST", "/v1/clear", `{"source":"tab1"}`, nil)
	var st Status
	resp := do(t, ts, "GET", "/v1/status", "", nil)
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if st.NowPlaying != nil {
		t.Errorf("clear did not remove the source: %+v", st.NowPlaying)
	}
}

func TestConfigEndpoint(t *testing.T) {
	ts, cfg := newTestServer(t)
	if resp := do(t, ts, "POST", "/v1/config", `{"clientId":"not-a-number"}`, nil); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad application ID must be refused, got %d", resp.StatusCode)
	}
	resp := do(t, ts, "POST", "/v1/config", `{"clientId":"123456789012345678","showButtons":false,"pauseTimeoutMinutes":9,"statusDisplay":"details","showPaused":true,"showAlbumArt":true}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if c := cfg.Get(); c.ClientID != "123456789012345678" || c.ShowButtons || c.PauseTimeoutMinutes != 9 || c.StatusDisplay != "details" {
		t.Errorf("config not applied: %+v", c)
	}
}

func TestValidHost(t *testing.T) {
	for h, want := range map[string]bool{
		"127.0.0.1:41713": true, "localhost:41713": true, "[::1]:41713": true, "127.0.0.1": true,
		"evil.com": false, "127.0.0.1.evil.com:41713": false, "evil.com:41713": false, "": false,
	} {
		if got := validHost(h); got != want {
			t.Errorf("validHost(%q) = %v, want %v", h, got, want)
		}
	}
}

func postVisibility(t *testing.T, ts *httptest.Server, body string) (int, VisibilityState) {
	t.Helper()
	resp := do(t, ts, "POST", "/v1/visibility", body, map[string]string{"Origin": "chrome-extension://abcdefghijklmnop"})
	var v VisibilityState
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func TestVisibilityHideShowToggle(t *testing.T) {
	ts, cfg := newTestServer(t)

	if code, v := postVisibility(t, ts, `{"action":"hide"}`); code != 200 || v.Enabled || v.Visible {
		t.Fatalf("hide: %d %+v", code, v)
	}
	if cfg.Get().Enabled {
		t.Error("hiding must be saved in the settings so it survives a restart")
	}

	if code, v := postVisibility(t, ts, `{"action":"toggle"}`); code != 200 || !v.Enabled || !v.Visible {
		t.Fatalf("toggle while hidden must show: %d %+v", code, v)
	}
	if code, v := postVisibility(t, ts, `{"action":"toggle"}`); code != 200 || v.Visible {
		t.Fatalf("toggle while visible must hide: %d %+v", code, v)
	}

	// A timed hide leaves the saved switch alone.
	postVisibility(t, ts, `{"action":"show"}`)
	code, v := postVisibility(t, ts, `{"action":"hide","minutes":60}`)
	if code != 200 || !v.Enabled || v.Visible || v.SnoozeSec < 59*60 || v.SnoozeSec > 60*60 {
		t.Fatalf("timed hide: %d %+v", code, v)
	}
	if !cfg.Get().Enabled {
		t.Error("a timed hide is not saved: a restart brings the presence back")
	}
	if code, v := postVisibility(t, ts, `{"action":"toggle"}`); code != 200 || !v.Visible || v.SnoozeSec != 0 {
		t.Fatalf("toggle during a timed hide must show again: %d %+v", code, v)
	}

	// Absurd lengths are capped at a day.
	if _, v := postVisibility(t, ts, `{"action":"hide","minutes":99999}`); v.SnoozeSec > 24*60*60 {
		t.Errorf("not capped: %d", v.SnoozeSec)
	}

	if code, _ := postVisibility(t, ts, `{"action":"explode"}`); code != http.StatusBadRequest {
		t.Errorf("unknown action must be rejected, got %d", code)
	}
}

func TestVisibilityRefusesWebPages(t *testing.T) {
	ts, cfg := newTestServer(t)
	resp := do(t, ts, "POST", "/v1/visibility", `{"action":"hide"}`, map[string]string{"Origin": "https://evil.example"})
	if resp.StatusCode != http.StatusForbidden || !cfg.Get().Enabled {
		t.Errorf("another website must not be able to switch the presence: %d", resp.StatusCode)
	}
}

func TestStatusAndUpdateReplyShowVisibility(t *testing.T) {
	ts, _ := newTestServer(t)
	postVisibility(t, ts, `{"action":"hide"}`)

	resp := do(t, ts, "POST", "/v1/update", goodUpdate, map[string]string{"Content-Type": "text/plain", "Origin": "chrome-extension://abcdefghijklmnop"})
	var reply struct {
		Visible *bool `json:"visible"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&reply)
	if reply.Visible == nil || *reply.Visible {
		t.Errorf("the extension's reply must say the presence is off (it shows a badge): %+v", reply)
	}

	var st Status
	resp = do(t, ts, "GET", "/v1/status", "", nil)
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if st.Visible || st.Config.Enabled || st.Hidden != presence.HiddenOff {
		t.Errorf("status must explain: visible=%v enabled=%v hidden=%q", st.Visible, st.Config.Enabled, st.Hidden)
	}
}

func TestConfigPostKeepsUntouchedSettings(t *testing.T) {
	ts, cfg := newTestServer(t)
	postVisibility(t, ts, `{"action":"hide"}`)

	// The dashboard's settings form doesn't send "enabled"; saving it must not switch the presence back on.
	resp := do(t, ts, "POST", "/v1/config", `{"privateMode":true,"hideMatching":["  Secret ","secret","x"]}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	got := cfg.Get()
	if got.Enabled || !got.PrivateMode || len(got.HideMatching) != 2 || got.HideMatching[0] != "secret" || !got.ShowProgress {
		t.Errorf("partial save wrong: %+v", got)
	}
}

func TestStatusSaysWhenTheExtensionCountsAsQuiet(t *testing.T) {
	ts, _ := newTestServer(t)
	var st Status
	resp := do(t, ts, "GET", "/v1/status", "", nil)
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	// The dashboard, popup and doctor use this instead of a number of their own,
	// so a paused tab that reports once a minute is not flagged as gone.
	if st.Extension.QuietAfterSec <= 65 {
		t.Errorf("quietAfterSec = %d, want well over a minute", st.Extension.QuietAfterSec)
	}
}

func TestDashboardIcon(t *testing.T) {
	ts, _ := newTestServer(t)
	resp := do(t, ts, "GET", "/icon.svg", "", nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("icon: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script") {
		t.Errorf("the icon must not be able to run scripts: %q", csp)
	}
}

// The dashboard's icon is a copy (Go can only embed its own folder's files);
// tools/gen_icons.py makes it. This catches an edited logo without the copy.
func TestDashboardIconMatchesTheLogo(t *testing.T) {
	want, err := os.ReadFile("../../assets/icon.svg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, iconSVG) {
		t.Error("internal/server/icon.svg differs from assets/icon.svg: run tools/gen_icons.py")
	}
}

// Settings changed from two places at once both stick.
func TestConcurrentConfigPosts(t *testing.T) {
	ts, cfg := newTestServer(t)
	done := make(chan struct{})
	go func() {
		do(t, ts, "POST", "/v1/config", `{"showPaused":false}`, nil)
		close(done)
	}()
	do(t, ts, "POST", "/v1/config", `{"privateMode":true}`, nil)
	<-done
	if c := cfg.Get(); c.ShowPaused || !c.PrivateMode {
		t.Errorf("a change was lost: %+v", c)
	}
	resp := do(t, ts, "POST", "/v1/config", `{"showPaused":`, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("broken JSON: want 400, got %d", resp.StatusCode)
	}
	if resp := do(t, ts, "POST", "/v1/config", ``, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("an empty body changes nothing and is fine, got %d", resp.StatusCode)
	}
}
