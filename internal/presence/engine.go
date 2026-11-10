package presence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
)

// Discord connection states, as reported on the dashboard and in `doctor`.
const (
	StateNoClientID   = "no-client-id"
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateDisconnected = "disconnected"
)

const (
	minSendInterval = 2 * time.Second // Discord rate-limits presence updates
	seekToleranceMs = 2500            // start-time drift that counts as a seek
	minBackoff      = 3 * time.Second
	maxBackoff      = 15 * time.Second
)

// Dialer connects to Discord. The real one is discord.Connect.
type Dialer func(ctx context.Context, clientID string) (discord.Conn, error)

type DiscordStatus struct {
	State     string `json:"state"`
	User      string `json:"user,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

type NowPlaying struct {
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Artwork  string  `json:"artwork"`
	VideoID  string  `json:"videoId"`
	State    string  `json:"state"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
}

type Snapshot struct {
	Discord        DiscordStatus
	PresenceActive bool
	NowPlaying     *NowPlaying
	// Hidden says why the song that is playing is not on Discord (one of the
	// Hidden* constants), or "" when it is shown or nothing is playing.
	Hidden string
	// SnoozeSec is how long a timed hide still has to run (0 = none).
	SnoozeSec int64
}

type sentState struct {
	key   string // identity of what Discord is showing ("" = nothing)
	start int64
	at    time.Time
}

// Engine keeps a Discord connection alive and makes Discord show whatever the
// tracker says is playing. It reconnects by itself when Discord is closed and
// reopened, so the user never has to restart anything.
type Engine struct {
	cfg  *config.Store
	tr   *Tracker
	dial Dialer
	now  func() time.Time
	wake chan struct{}

	mu          sync.Mutex // guards status, active and snoozeUntil
	status      DiscordStatus
	active      bool
	snoozeUntil time.Time // a timed hide; not saved, so a restart ends it

	// Only touched from the Run goroutine (or tests calling step directly).
	conn     discord.Conn
	connID   string
	nextDial time.Time
	backoff  time.Duration
	sent     sentState
	basic    bool
}

func NewEngine(cfg *config.Store, tr *Tracker, dial Dialer) *Engine {
	return &Engine{
		cfg:     cfg,
		tr:      tr,
		dial:    dial,
		now:     time.Now,
		wake:    make(chan struct{}, 1),
		backoff: minBackoff,
		status:  DiscordStatus{State: StateConnecting},
	}
}

// Wake makes the engine re-evaluate immediately (a new update arrived).
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// InjectTest shows a fake track for 30 seconds, so people can check the
// Discord side without opening YouTube Music.
func (e *Engine) InjectTest() {
	e.tr.Put(Update{
		Source: TestSource, Agent: "self-test", State: StatePlaying,
		Title: "Test song", Artist: "YTM Beacon self-test", Album: "If you can see this, Discord works",
		Position: 30, Duration: 200,
	})
	e.Wake()
}

// HideFor keeps everything off Discord for d, then shows it again by itself.
func (e *Engine) HideFor(d time.Duration) {
	e.mu.Lock()
	e.snoozeUntil = e.now().Add(d)
	e.mu.Unlock()
	e.Wake()
}

// CancelHide ends a timed hide early.
func (e *Engine) CancelHide() {
	e.mu.Lock()
	e.snoozeUntil = time.Time{}
	e.mu.Unlock()
	e.Wake()
}

// SnoozeLeft is how long a timed hide still has to run (0 = none).
func (e *Engine) SnoozeLeft() time.Duration { return e.snoozeLeft(e.now()) }

func (e *Engine) snoozeLeft(now time.Time) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d := e.snoozeUntil.Sub(now); d > 0 && !e.snoozeUntil.IsZero() {
		return d
	}
	return 0
}

// hiddenFor says why the settings or the user's switches keep this song off
// Discord ("" = show it).
func (e *Engine) hiddenFor(cfg config.Config, sel Selected, now time.Time) string {
	switch {
	case !cfg.Enabled:
		return HiddenOff
	case e.snoozeLeft(now) > 0:
		return HiddenSnoozed
	}
	return hiddenReason(cfg, sel, now)
}

func (e *Engine) Snapshot() Snapshot {
	now := e.now()
	e.mu.Lock()
	s := Snapshot{Discord: e.status, PresenceActive: e.active}
	e.mu.Unlock()
	s.SnoozeSec = int64(e.snoozeLeft(now).Round(time.Second) / time.Second)
	if sel, ok := e.tr.Select(); ok {
		s.NowPlaying = &NowPlaying{
			Title: sel.Title, Artist: sel.Artist, Album: sel.Album, Artwork: sel.Artwork,
			VideoID: sel.VideoID, State: sel.State, Position: sel.Position, Duration: sel.Duration,
		}
		s.Hidden = e.hiddenFor(e.cfg.Get(), sel, now)
	}
	return s
}

func (e *Engine) setStatus(f func(*DiscordStatus)) {
	e.mu.Lock()
	f(&e.status)
	e.mu.Unlock()
}

func (e *Engine) setActive(v bool) {
	e.mu.Lock()
	e.active = v
	e.mu.Unlock()
}

// Run blocks until ctx is cancelled, then clears the presence and disconnects.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		e.step(ctx)
		var done <-chan struct{}
		if e.conn != nil {
			done = e.conn.Done()
		}
		select {
		case <-ctx.Done():
			e.shutdown()
			return
		case <-t.C:
		case <-e.wake:
		case <-done:
		}
	}
}

func (e *Engine) shutdown() {
	if e.conn != nil {
		_ = e.conn.SetActivity(nil)
		_ = e.conn.Close()
		e.conn = nil
	}
	e.setActive(false)
}

func (e *Engine) dropConn() {
	if e.conn != nil {
		_ = e.conn.Close()
		e.conn = nil
	}
	e.setActive(false)
}

// step does one round of: make sure we are connected, work out what Discord
// should show, and send it if it differs from what we sent last.
func (e *Engine) step(ctx context.Context) {
	now := e.now()
	cfg := e.cfg.Get()
	id := cfg.EffectiveClientID()

	if id == "" {
		e.dropConn()
		e.setStatus(func(d *DiscordStatus) { *d = DiscordStatus{State: StateNoClientID} })
		return
	}
	if e.conn != nil && e.connID != id {
		e.dropConn() // application ID changed in the settings
	}
	if e.conn != nil {
		select {
		case <-e.conn.Done():
			slog.Info("lost connection to Discord; will reconnect")
			e.dropConn()
			e.setStatus(func(d *DiscordStatus) {
				*d = DiscordStatus{State: StateDisconnected, LastError: "the connection to Discord was closed"}
			})
			e.nextDial = now
		default:
		}
	}

	if e.conn == nil {
		if now.Before(e.nextDial) {
			return
		}
		c, err := e.dial(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.nextDial = now.Add(e.backoff)
			if e.backoff *= 2; e.backoff > maxBackoff {
				e.backoff = maxBackoff
			}
			msg := err.Error()
			e.setStatus(func(d *DiscordStatus) {
				if d.LastError != msg {
					slog.Warn("cannot connect to Discord", "error", msg)
				}
				*d = DiscordStatus{State: StateDisconnected, LastError: msg}
			})
			return
		}
		e.conn, e.connID = c, id
		e.backoff = minBackoff
		e.sent = sentState{}
		// A newer Discord may have been started since an older one made us fall
		// back to the plain card, so try the full one again on every connection.
		e.basic = false
		info := c.Info()
		slog.Info("connected to Discord", "user", info.User, "endpoint", info.Endpoint)
		e.setStatus(func(d *DiscordStatus) {
			*d = DiscordStatus{State: StateConnected, User: info.User, Endpoint: info.Endpoint}
		})
	}

	sel, ok := e.tr.Select()
	var act *discord.Activity
	var start int64
	if ok && e.hiddenFor(cfg, sel, now) == "" {
		act, start = BuildActivity(cfg, sel, now, e.basic)
	}
	key := activityKey(act)
	if !e.needsSend(key, start, now) {
		return
	}
	e.send(cfg, sel, ok, act, key, start, now)
}

func (e *Engine) needsSend(key string, start int64, now time.Time) bool {
	changed := key != e.sent.key
	if !changed && key != "" && start != 0 && e.sent.start != 0 {
		d := start - e.sent.start
		if d < 0 {
			d = -d
		}
		changed = d > seekToleranceMs
	}
	if !changed {
		return false
	}
	return e.sent.at.IsZero() || now.Sub(e.sent.at) >= minSendInterval
}

func (e *Engine) send(cfg config.Config, sel Selected, ok bool, act *discord.Activity, key string, start int64, now time.Time) {
	err := e.conn.SetActivity(act)

	var apiErr *discord.APIError
	if errors.As(err, &apiErr) && act != nil && !e.basic {
		// Some Discord clients reject newer optional fields. Retry once with
		// the plain version and keep using it.
		slog.Warn("Discord rejected the presence; switching to a simpler one", "error", err)
		e.basic = true
		if ok {
			act, start = BuildActivity(cfg, sel, now, true)
		}
		key = activityKey(act)
		err = e.conn.SetActivity(act)
	}

	if err != nil {
		if errors.As(err, &apiErr) {
			msg := "Discord would not accept the presence: " + err.Error()
			slog.Error(msg)
			e.setStatus(func(d *DiscordStatus) { d.LastError = msg })
			e.sent = sentState{key: key, start: start, at: now} // do not hammer Discord
			return
		}
		slog.Warn("lost connection while updating presence", "error", err)
		e.dropConn()
		e.setStatus(func(d *DiscordStatus) { *d = DiscordStatus{State: StateDisconnected, LastError: err.Error()} })
		e.nextDial = now
		return
	}

	e.sent = sentState{key: key, start: start, at: now}
	e.setActive(act != nil)
	e.setStatus(func(d *DiscordStatus) { d.LastError = "" })
	if act != nil {
		slog.Debug("presence updated", "title", sel.Title, "artist", sel.Artist, "state", sel.State)
	} else {
		slog.Debug("presence cleared")
	}
}

// activityKey identifies what an activity shows, ignoring the exact start
// time (which drifts by a few milliseconds between heartbeats).
func activityKey(a *discord.Activity) string {
	if a == nil {
		return ""
	}
	c := *a
	hasStart, hasEnd := false, false
	if c.Timestamps != nil {
		hasStart, hasEnd = c.Timestamps.Start != 0, c.Timestamps.End != 0
		c.Timestamps = nil
	}
	b, _ := json.Marshal(c)
	return fmt.Sprintf("%s|%t|%t", b, hasStart, hasEnd)
}
