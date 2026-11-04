// Package presence decides what Discord should show, based on what the browser
// extension reports, and keeps Discord up to date.
package presence

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	StatePlaying = "playing"
	StatePaused  = "paused"

	// TestSource is the fake source used by the dashboard's "send test" button.
	TestSource = "test"

	// A source that stops reporting is forgotten after this long (browser
	// closed, tab crashed...). The extension reports every 5 seconds.
	sourceTTL = 45 * time.Second
	testTTL   = 30 * time.Second
)

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Update is what the browser extension sends about one YouTube Music tab.
type Update struct {
	Source   string  `json:"source"` // unique per tab/page load
	Agent    string  `json:"agent,omitempty"`
	State    string  `json:"state"` // "playing" or "paused"
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Artwork  string  `json:"artwork"`
	VideoID  string  `json:"videoId"`
	Position float64 `json:"position"` // seconds
	Duration float64 `json:"duration"` // seconds, 0 if unknown
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func cleanNumber(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0
	}
	if f > 1e6 {
		return 1e6
	}
	return f
}

// Sanitize makes an update safe to use. The extension is the only expected
// sender, but this keeps odd or oversized input from reaching Discord.
func (u *Update) Sanitize() error {
	u.Source = truncate(u.Source, 64)
	if u.Source == "" {
		return errors.New("missing source")
	}
	u.Agent = truncate(u.Agent, 32)
	u.Title = truncate(u.Title, 300)
	if u.Title == "" {
		return errors.New("missing title")
	}
	u.Artist = truncate(u.Artist, 300)
	u.Album = truncate(u.Album, 300)
	if u.State != StatePaused {
		u.State = StatePlaying
	}
	u.Artwork = strings.TrimSpace(u.Artwork)
	if !strings.HasPrefix(u.Artwork, "https://") || len(u.Artwork) > 500 {
		u.Artwork = ""
	}
	if !videoIDPattern.MatchString(u.VideoID) {
		u.VideoID = ""
	}
	u.Position = cleanNumber(u.Position)
	u.Duration = cleanNumber(u.Duration)
	return nil
}

// Selected is the source the app is currently following.
type Selected struct {
	Update
	ReceivedAt  time.Time // when the position in Update was measured
	PausedSince time.Time // zero while playing
}

type entry struct {
	u           Update
	at          time.Time
	expires     time.Time
	pausedSince time.Time
}

// SourceInfo describes one reporting tab, for the dashboard.
type SourceInfo struct {
	ID     string `json:"id"`
	Agent  string `json:"agent"`
	State  string `json:"state"`
	AgeSec int64  `json:"ageSec"`
}

// Tracker remembers the latest report from every YouTube Music tab and picks
// the one to show: a playing tab beats a paused one, newest wins ties.
type Tracker struct {
	mu      sync.Mutex
	now     func() time.Time
	m       map[string]*entry
	lastExt time.Time
}

func NewTracker() *Tracker { return NewTrackerWithClock(time.Now) }

func NewTrackerWithClock(now func() time.Time) *Tracker {
	return &Tracker{now: now, m: map[string]*entry{}}
}

// Put records an update that has already been sanitized.
func (t *Tracker) Put(u Update) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	ttl := sourceTTL
	if u.Source == TestSource {
		ttl = testTTL
	} else {
		t.lastExt = now
	}
	e := t.m[u.Source]
	if e == nil {
		e = &entry{}
		t.m[u.Source] = e
	}
	if u.State == StatePaused {
		// Keep the original pause time across repeated "still paused" reports.
		if e.u.State != StatePaused || e.u.Title != u.Title || e.pausedSince.IsZero() {
			e.pausedSince = now
		}
	} else {
		e.pausedSince = time.Time{}
	}
	e.u, e.at, e.expires = u, now, now.Add(ttl)
}

func (t *Tracker) Clear(source string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, source)
}

func (t *Tracker) prune(now time.Time) {
	for k, e := range t.m {
		if now.After(e.expires) {
			delete(t.m, k)
		}
	}
}

// Select returns the source to show, if any.
func (t *Tracker) Select() (Selected, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(t.now())
	var best *entry
	for _, e := range t.m {
		if best == nil {
			best = e
			continue
		}
		bp, ep := best.u.State == StatePlaying, e.u.State == StatePlaying
		if (ep && !bp) || (ep == bp && e.at.After(best.at)) {
			best = e
		}
	}
	if best == nil {
		return Selected{}, false
	}
	return Selected{Update: best.u, ReceivedAt: best.at, PausedSince: best.pausedSince}, true
}

// LastExtensionSeen is when any browser extension last reported in.
func (t *Tracker) LastExtensionSeen() (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastExt, !t.lastExt.IsZero()
}

func (t *Tracker) Sources() []SourceInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	t.prune(now)
	out := make([]SourceInfo, 0, len(t.m))
	for k, e := range t.m {
		id := k
		if len(id) > 8 {
			id = id[:8]
		}
		out = append(out, SourceInfo{ID: id, Agent: e.u.Agent, State: e.u.State, AgeSec: int64(now.Sub(e.at).Seconds())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
