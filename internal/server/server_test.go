package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
