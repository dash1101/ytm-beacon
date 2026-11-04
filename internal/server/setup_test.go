package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
	"github.com/dash1101/ytm-beacon/internal/presence"
)

func newSetupServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	t.Setenv("YTM_BEACON_HOME", t.TempDir())
	marker := filepath.Join(t.TempDir(), "data", "extension-connected")
	cfg := config.NewMemoryStore(config.Default())
	tr := presence.NewTracker()
	eng := presence.NewEngine(cfg, tr, func(context.Context, string) (discord.Conn, error) {
		return nil, errors.New("no discord in tests")
	})
	srv := New(Deps{Config: cfg, Tracker: tr, Engine: eng, ExtensionBuild: "abc123def456", ConnectedMarker: marker}, 41713)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, marker
}

func TestFirstRunMarkerAndBuildAnnouncement(t *testing.T) {
	ts, marker := newSetupServer(t)

	status := func() Status {
		var st Status
		resp := do(t, ts, "GET", "/v1/status", "", nil)
		if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}

	st := status()
	if st.Extension.EverConnected {
		t.Error("nothing has connected yet, the dashboard should show the setup steps")
	}
	if st.ExtensionBuild != "abc123def456" {
		t.Errorf("status must announce the extension build, got %q", st.ExtensionBuild)
	}
	for _, b := range []string{"chrome", "firefox"} {
		if d := st.ExtensionDirs[b]; d == "" || !strings.Contains(d, b) {
			t.Errorf("extensionDirs[%s] = %q, the setup page needs the folder to load", b, d)
		}
	}

	// The extension reports in; the reply tells it which build the app ships.
	resp := do(t, ts, "POST", "/v1/update", goodUpdate, map[string]string{"Content-Type": "text/plain"})
	var reply struct {
		OK             bool   `json:"ok"`
		ExtensionBuild string `json:"extensionBuild"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, err %v", resp.StatusCode, err)
	}
	if !reply.OK || reply.ExtensionBuild != "abc123def456" {
		t.Errorf("update reply = %+v; the extension reloads itself when the build differs from its own", reply)
	}

	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the first report must create the marker so setup steps stop showing: %v", err)
	}
	if !status().Extension.EverConnected {
		t.Error("everConnected must be true after the first report")
	}
}
