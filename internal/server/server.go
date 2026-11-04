// Package server is the small local web server the browser extension talks to.
// It only listens on 127.0.0.1 and refuses requests from ordinary web pages,
// so other websites cannot read your status or fake what you're listening to.
package server

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/paths"
	"github.com/dash1101/ytm-beacon/internal/presence"
	"github.com/dash1101/ytm-beacon/internal/version"
)

//go:embed dashboard.html
var dashboardHTML []byte

// Ports the app may listen on. The extension tries them in order, so a busy
// port never stops the app from working. Keep in sync with extension/*.js.
var Ports = []int{41713, 41714, 41715, 41716, 41717}

// ErrAlreadyRunning is returned by Listen when another copy is already up.
var ErrAlreadyRunning = errors.New("YTM Beacon is already running")

// Deps are the pieces the server needs.
type Deps struct {
	Config  *config.Store
	Tracker *presence.Tracker
	Engine  *presence.Engine
	LogPath string
	Quit    func() // asks the whole app to shut down

	// ExtensionBuild identifies the extension code this app ships. It is sent
	// back to the extension, which reloads itself when it is out of date.
	ExtensionBuild string
	// ConnectedMarker is a file created the first time an extension reports in.
	ConnectedMarker string
}

type Server struct {
	d       Deps
	port    int
	started time.Time
}

func New(d Deps, port int) *Server { return &Server{d: d, port: port, started: time.Now()} }

// Status is what /v1/status returns; the popup, dashboard and `doctor` use it.
type Status struct {
	App            string                 `json:"app"`
	Version        string                 `json:"version"`
	Commit         string                 `json:"commit"`
	OS             string                 `json:"os"`
	Arch           string                 `json:"arch"`
	PID            int                    `json:"pid"`
	Port           int                    `json:"port"`
	UptimeSec      int64                  `json:"uptimeSec"`
	ClientIDSet    bool                   `json:"clientIdSet"`
	Discord        presence.DiscordStatus `json:"discord"`
	Extension      ExtensionStatus        `json:"extension"`
	NowPlaying     *presence.NowPlaying   `json:"nowPlaying"`
	PresenceActive bool                   `json:"presenceActive"`
	Config         config.Config          `json:"config"`
	ConfigPath     string                 `json:"configPath"`
	LogPath        string                 `json:"logPath"`
	ExtensionBuild string                 `json:"extensionBuild"`
	// ExtensionDirs are the folders to load into the browser, by flavour.
	ExtensionDirs map[string]string `json:"extensionDirs"`
}

type ExtensionStatus struct {
	LastSeenAgoSec *int64                `json:"lastSeenAgoSec"` // null until an extension has reported in
	Sources        []presence.SourceInfo `json:"sources"`
	// EverConnected is true once an extension has reported in, even before this
	// run of the app. The dashboard shows first-run setup steps until then.
	EverConnected bool `json:"everConnected"`
}

func (s *Server) buildStatus() Status {
	cfg := s.d.Config.Get()
	snap := s.d.Engine.Snapshot()
	st := Status{
		App:            "ytm-beacon",
		Version:        version.Version,
		Commit:         version.Commit,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		PID:            os.Getpid(),
		Port:           s.port,
		UptimeSec:      int64(time.Since(s.started).Seconds()),
		ClientIDSet:    cfg.EffectiveClientID() != "",
		Discord:        snap.Discord,
		NowPlaying:     snap.NowPlaying,
		PresenceActive: snap.PresenceActive,
		Config:         cfg,
		ConfigPath:     s.d.Config.Path(),
		LogPath:        s.d.LogPath,
		Extension:      ExtensionStatus{Sources: s.d.Tracker.Sources(), EverConnected: fileExists(s.d.ConnectedMarker)},
		ExtensionBuild: s.d.ExtensionBuild,
		ExtensionDirs:  map[string]string{},
	}
	for _, b := range []string{"chrome", "firefox"} {
		if dir, err := paths.ExtensionDir(b); err == nil {
			st.ExtensionDirs[b] = dir
		}
	}
	if t, ok := s.d.Tracker.LastExtensionSeen(); ok {
		ago := int64(time.Since(t).Seconds())
		st.Extension.LastSeenAgoSec = &ago
	}
	return st
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.dashboard)
	mux.HandleFunc("GET /v1/ping", s.ping)
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("POST /v1/update", s.update)
	mux.HandleFunc("POST /v1/clear", s.clear)
	mux.HandleFunc("GET /v1/config", s.getConfig)
	mux.HandleFunc("POST /v1/config", s.setConfig)
	mux.HandleFunc("POST /v1/test", s.test)
	mux.HandleFunc("GET /v1/logs", s.logs)
	mux.HandleFunc("POST /v1/quit", s.quit)
	return s.guard(mux)
}

func validHost(h string) bool {
	host := h
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.HasSuffix(h, "]") {
		host = h[:i]
	}
	switch host {
	case "127.0.0.1", "localhost", "[::1]":
		return true
	}
	return false
}

func isExtensionOrigin(o string) bool {
	return strings.HasPrefix(o, "chrome-extension://") || strings.HasPrefix(o, "moz-extension://")
}

// guard is the security gate in front of every endpoint:
//   - The Host header must be a loopback name (stops DNS-rebinding attacks).
//   - Browser extensions are allowed (and get CORS headers); our own dashboard
//     is allowed; any other website that sends an Origin header is refused.
//   - Requests with no Origin (curl, the `doctor` command) are fine.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			switch {
			case isExtensionOrigin(origin):
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type")
			case origin == "http://"+r.Host:
				// the dashboard itself
			default:
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	body := http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return false
	}
	return true
}

func (s *Server) dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src https: data:; connect-src 'self'")
	_, _ = w.Write(dashboardHTML)
}

func (s *Server) ping(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"app": "ytm-beacon", "version": version.Version})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.buildStatus())
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	var u presence.Update
	if !readJSON(w, r, &u) {
		return
	}
	if err := u.Sanitize(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if u.Source == presence.TestSource {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "reserved source name"})
		return
	}
	if s.d.Tracker.Put(u) {
		slog.Info("browser extension connected", "browser", u.Agent) // no song details: the log may be shared in bug reports
		s.markConnected()
	}
	s.d.Engine.Wake()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "extensionBuild": s.d.ExtensionBuild})
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// markConnected remembers that setup is done, for the dashboard and installer.
func (s *Server) markConnected() {
	if s.d.ConnectedMarker == "" || fileExists(s.d.ConnectedMarker) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.d.ConnectedMarker), 0o755); err == nil {
		_ = os.WriteFile(s.d.ConnectedMarker, nil, 0o644)
	}
}

func (s *Server) clear(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Source string `json:"source"`
	}
	if !readJSON(w, r, &b) {
		return
	}
	s.d.Tracker.Clear(truncateSource(b.Source))
	s.d.Engine.Wake()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func truncateSource(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 64 {
		return s[:64]
	}
	return s
}

func (s *Server) getConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.d.Config.Get())
}

func (s *Server) setConfig(w http.ResponseWriter, r *http.Request) {
	c := s.d.Config.Get()
	if !readJSON(w, r, &c) {
		return
	}
	if err := s.d.Config.Set(c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.d.Engine.Wake()
	writeJSON(w, http.StatusOK, s.d.Config.Get())
}

func (s *Server) test(w http.ResponseWriter, _ *http.Request) {
	s.d.Engine.InjectTest()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) logs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, TailFile(s.d.LogPath, 200))
}

func (s *Server) quit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	if s.d.Quit != nil {
		go func() {
			time.Sleep(100 * time.Millisecond) // let the reply go out first
			s.d.Quit()
		}()
	}
}

// TailFile returns the last n lines of a text file ("" if it can't be read).
func TailFile(path string, n int) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const window = 64 << 10 // the last 64 KB is plenty for 200 lines
	if fi, err := f.Stat(); err == nil && fi.Size() > window {
		_, _ = f.Seek(-window, io.SeekEnd)
	}
	b, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n") + "\n"
}

// Listen picks the first free port. If a YTM Beacon is already answering on
// one of the ports it returns ErrAlreadyRunning (and that port number).
func Listen() (net.Listener, int, error) {
	for _, p := range Ports {
		if Ping(p) {
			return nil, p, ErrAlreadyRunning
		}
	}
	var lastErr error
	for i, p := range Ports {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			lastErr = err
			continue
		}
		// Two copies starting at the same moment could each have taken a
		// different port; the one on the higher port backs off.
		for _, lower := range Ports[:i] {
			if Ping(lower) {
				_ = l.Close()
				return nil, lower, ErrAlreadyRunning
			}
		}
		return l, p, nil
	}
	return nil, 0, fmt.Errorf("ports %d-%d are all in use by other programs: %w", Ports[0], Ports[len(Ports)-1], lastErr)
}

var client = &http.Client{Timeout: 1500 * time.Millisecond}

func get(port int, path string) (*http.Response, error) {
	return client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
}

// Ping reports whether a YTM Beacon is answering on the port.
func Ping(port int) bool {
	resp, err := get(port, "/v1/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var b struct {
		App string `json:"app"`
	}
	return json.NewDecoder(resp.Body).Decode(&b) == nil && b.App == "ytm-beacon"
}

// FetchStatus asks a running app for its status. The error is non-nil when no
// copy is running.
func FetchStatus() (*Status, error) {
	for _, p := range Ports {
		resp, err := get(p, "/v1/status")
		if err != nil {
			continue
		}
		var st Status
		err = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		if err == nil && st.App == "ytm-beacon" {
			return &st, nil
		}
	}
	return nil, errors.New("YTM Beacon is not running")
}

// Quit asks a running app to shut down and waits until it has.
func Quit() error {
	st, err := FetchStatus()
	if err != nil {
		return nil // nothing to stop
	}
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/v1/quit", st.Port), "application/json", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	resp.Body.Close()
	for i := 0; i < 50; i++ {
		if !Ping(st.Port) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the running app did not stop in time")
}
