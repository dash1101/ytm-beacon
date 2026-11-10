package presence

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
)

// ---- helpers ----------------------------------------------------------------

type clock struct{ t time.Time }

func (c *clock) Now() time.Time          { return c.t }
func (c *clock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *clock { return &clock{t: time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)} }

func testConfig() config.Config {
	c := config.Default()
	c.ClientID = "123456789012345678"
	return c
}

type fakeConn struct {
	mu    sync.Mutex
	calls []*discord.Activity
	fail  func(*discord.Activity) error
	done  chan struct{}
	once  sync.Once
}

func newFakeConn() *fakeConn { return &fakeConn{done: make(chan struct{})} }

func (f *fakeConn) SetActivity(a *discord.Activity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		if err := f.fail(a); err != nil {
			return err
		}
	}
	f.calls = append(f.calls, a)
	return nil
}
func (f *fakeConn) Done() <-chan struct{} { return f.done }
func (f *fakeConn) Close() error          { f.once.Do(func() { close(f.done) }); return nil }
func (f *fakeConn) Info() discord.Info    { return discord.Info{Endpoint: "fake", User: "Tester"} }
func (f *fakeConn) count() int            { f.mu.Lock(); defer f.mu.Unlock(); return len(f.calls) }
func (f *fakeConn) last() *discord.Activity {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return nil
	}
	return f.calls[len(f.calls)-1]
}

type harness struct {
	clk   *clock
	tr    *Tracker
	cfg   *config.Store
	eng   *Engine
	conns []*fakeConn
	dials int
	// dialErr, when set, makes the next dials fail.
	dialErr error
	onNew   func(*fakeConn)
}

func newHarness(cfg config.Config) *harness {
	h := &harness{clk: newClock()}
	h.tr = NewTrackerWithClock(h.clk.Now)
	h.cfg = config.NewMemoryStore(cfg)
	h.eng = NewEngine(h.cfg, h.tr, func(ctx context.Context, id string) (discord.Conn, error) {
		h.dials++
		if h.dialErr != nil {
			return nil, h.dialErr
		}
		c := newFakeConn()
		if h.onNew != nil {
			h.onNew(c)
		}
		h.conns = append(h.conns, c)
		return c, nil
	})
	h.eng.now = h.clk.Now
	return h
}

func (h *harness) conn() *fakeConn { return h.conns[len(h.conns)-1] }
func (h *harness) step()           { h.eng.step(context.Background()) }

func song(source, title string, state string, pos, dur float64) Update {
	return Update{Source: source, Agent: "Test", State: state, Title: title, Artist: "Artist", Album: "Album",
		Artwork: "https://lh3.googleusercontent.com/x=w544-h544", VideoID: "dQw4w9WgXcQ", Position: pos, Duration: dur}
}

// ---- BuildActivity -----------------------------------------------------------

func TestBuildPlaying(t *testing.T) {
	now := newClock().t
	sel := Selected{Update: song("a", "Song", StatePlaying, 30, 200), ReceivedAt: now}
	act, start := BuildActivity(config.Default(), sel, now, false)
	if act == nil {
		t.Fatal("nil activity")
	}
	if act.Type != discord.TypeListening || act.Details != "Song" || act.State != "Artist" {
		t.Errorf("bad text: %+v", act)
	}
	wantStart := now.Add(-30 * time.Second).UnixMilli()
	if start != wantStart || act.Timestamps.Start != wantStart || act.Timestamps.End != wantStart+200_000 {
		t.Errorf("bad timestamps: start=%d %+v", start, act.Timestamps)
	}
	if act.Assets.LargeImage != sel.Artwork || act.Assets.LargeText != "Album" || act.Assets.SmallImage != assetPlay {
		t.Errorf("bad assets: %+v", act.Assets)
	}
	if len(act.Buttons) != 1 || act.Buttons[0].URL != "https://music.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Errorf("bad buttons: %+v", act.Buttons)
	}
	if act.StatusDisplayType != nil {
		t.Error("default status display must not send status_display_type")
	}
}

func TestBuildPausedAndTimeout(t *testing.T) {
	clk := newClock()
	sel := Selected{Update: song("a", "Song", StatePaused, 30, 200), ReceivedAt: clk.t, PausedSince: clk.t}

	act, start := BuildActivity(config.Default(), sel, clk.t.Add(time.Minute), false)
	if act == nil || act.Timestamps != nil || start != 0 || act.Assets.SmallImage != assetPause {
		t.Errorf("paused card wrong: %+v start=%d", act, start)
	}

	if act, _ := BuildActivity(config.Default(), sel, clk.t.Add(6*time.Minute), false); act != nil {
		t.Error("paused longer than the timeout must be hidden")
	}
	cfg := config.Default()
	cfg.ShowPaused = false
	if act, _ := BuildActivity(cfg, sel, clk.t, false); act != nil {
		t.Error("ShowPaused=false must hide immediately")
	}
}

func TestBuildOptions(t *testing.T) {
	now := newClock().t
	sel := Selected{Update: song("a", "S", StatePlaying, 0, 0), ReceivedAt: now}
	sel.Artist = ""
	sel.VideoID = ""
	sel.Artwork = ""

	cfg := config.Default()
	cfg.ShowButtons = false
	cfg.StatusDisplay = config.StatusDetails
	act, _ := BuildActivity(cfg, sel, now, false)
	if act.Details != "S​" {
		t.Errorf("one-character fields must be padded, got %q", act.Details)
	}
	if act.State != "" || len(act.Buttons) != 0 {
		t.Errorf("empty artist / buttons off: %+v", act)
	}
	if act.Timestamps.End != 0 {
		t.Error("unknown duration must not produce an end time")
	}
	if act.Assets.LargeImage != assetLogo {
		t.Errorf("fallback logo expected, got %+v", act.Assets)
	}
	if act.StatusDisplayType == nil || *act.StatusDisplayType != discord.StatusDisplayDetails {
		t.Errorf("status display not applied: %v", act.StatusDisplayType)
	}

	basic, _ := BuildActivity(cfg, sel, now, true)
	if basic.StatusDisplayType != nil || basic.Assets != nil {
		t.Errorf("basic mode must drop optional fields: %+v %+v", basic, basic.Assets)
	}
}

func TestSearchLinkWhenNoVideoID(t *testing.T) {
	sel := Selected{Update: Update{Artist: "Daft Punk", Title: "One More Time"}}
	got := trackURL(sel)
	if !strings.HasPrefix(got, "https://music.youtube.com/search?q=Daft+Punk+One+More+Time") {
		t.Errorf("got %s", got)
	}
}

func TestFieldTruncates(t *testing.T) {
	long := strings.Repeat("あ", 300)
	if n := len([]rune(field(long))); n != 100 {
		t.Errorf("want 100 runes, got %d", n)
	}
}

// ---- Update.Sanitize ---------------------------------------------------------

func TestSanitize(t *testing.T) {
	u := Update{Source: "s", Title: "  T  ", State: "weird", Artwork: "http://insecure/x.png", VideoID: "nope", Position: -5, Duration: 1e12}
	if err := u.Sanitize(); err != nil {
		t.Fatal(err)
	}
	if u.Title != "T" || u.State != StatePlaying || u.Artwork != "" || u.VideoID != "" || u.Position != 0 || u.Duration != 1e6 {
		t.Errorf("not sanitized: %+v", u)
	}
	if (&Update{Source: "s"}).Sanitize() == nil {
		t.Error("missing title must be rejected")
	}
	if (&Update{Title: "t"}).Sanitize() == nil {
		t.Error("missing source must be rejected")
	}
}

// ---- Tracker -------------------------------------------------------------------

func TestTrackerPrefersPlayingThenNewest(t *testing.T) {
	clk := newClock()
	tr := NewTrackerWithClock(clk.Now)
	tr.Put(song("a", "A", StatePaused, 0, 0))
	clk.Advance(time.Second)
	tr.Put(song("b", "B", StatePlaying, 0, 0))
	clk.Advance(time.Second)
	tr.Put(song("c", "C", StatePaused, 0, 0))
	if sel, _ := tr.Select(); sel.Title != "B" {
		t.Errorf("playing must beat paused, got %s", sel.Title)
	}
	clk.Advance(time.Second)
	tr.Put(song("a", "A", StatePlaying, 0, 0))
	if sel, _ := tr.Select(); sel.Title != "A" {
		t.Errorf("newest playing must win, got %s", sel.Title)
	}
}

func TestTrackerExpiryAndClear(t *testing.T) {
	clk := newClock()
	tr := NewTrackerWithClock(clk.Now)
	tr.Put(song("a", "A", StatePlaying, 0, 0))
	if _, seen := tr.LastExtensionSeen(); !seen {
		t.Error("extension should be marked as seen")
	}
	clk.Advance(sourceTTL + time.Second)
	if _, ok := tr.Select(); ok {
		t.Error("stale source must expire")
	}
	if _, seen := tr.LastExtensionSeen(); !seen {
		t.Error("last-seen must survive expiry")
	}
	tr.Put(song("a", "A", StatePlaying, 0, 0))
	tr.Clear("a")
	if _, ok := tr.Select(); ok {
		t.Error("cleared source must be gone")
	}
}

func TestTrackerKeepsOriginalPauseTime(t *testing.T) {
	clk := newClock()
	tr := NewTrackerWithClock(clk.Now)
	tr.Put(song("a", "A", StatePaused, 5, 100))
	first := clk.t
	clk.Advance(20 * time.Second)
	tr.Put(song("a", "A", StatePaused, 5, 100))
	sel, _ := tr.Select()
	if !sel.PausedSince.Equal(first) {
		t.Errorf("PausedSince moved: %v", sel.PausedSince)
	}
	tr.Put(song("a", "A", StatePlaying, 6, 100))
	if sel, _ := tr.Select(); !sel.PausedSince.IsZero() {
		t.Error("PausedSince must reset when playing")
	}
}

func TestTestSourceDoesNotCountAsExtension(t *testing.T) {
	tr := NewTrackerWithClock(newClock().Now)
	tr.Put(Update{Source: TestSource, State: StatePlaying, Title: "T"})
	if _, seen := tr.LastExtensionSeen(); seen {
		t.Error("the self-test must not look like a browser extension")
	}
}

// ---- Engine --------------------------------------------------------------------

func TestEngineNoClientID(t *testing.T) {
	cfg := config.Default() // no client id of its own
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	old := config.DefaultClientID
	config.DefaultClientID = "" // and no built-in default either
	t.Cleanup(func() { config.DefaultClientID = old })
	h := newHarness(cfg)
	h.step()
	if h.dials != 0 || h.eng.Snapshot().Discord.State != StateNoClientID {
		t.Errorf("want no-client-id and no dialing, got %+v dials=%d", h.eng.Snapshot().Discord, h.dials)
	}
}

func TestEngineSendsOnlyOnChange(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())

	h.step() // connects, nothing playing
	if st := h.eng.Snapshot().Discord; st.State != StateConnected || st.User != "Tester" {
		t.Fatalf("not connected: %+v", st)
	}
	if h.conn().count() != 0 {
		t.Fatal("nothing is playing, nothing should be sent")
	}

	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()
	if h.conn().count() != 1 || h.conn().last().Details != "One" || !h.eng.Snapshot().PresenceActive {
		t.Fatalf("first track not shown: %d calls", h.conn().count())
	}

	// Heartbeats with matching positions: no new sends.
	for i := 0; i < 4; i++ {
		h.clk.Advance(5 * time.Second)
		h.tr.Put(song("a", "One", StatePlaying, 10+float64(5*(i+1)), 200))
		h.step()
	}
	if h.conn().count() != 1 {
		t.Errorf("heartbeats must not cause updates, got %d calls", h.conn().count())
	}

	// A seek forward is noticed.
	h.clk.Advance(5 * time.Second)
	h.tr.Put(song("a", "One", StatePlaying, 120, 200))
	h.step()
	if h.conn().count() != 2 {
		t.Errorf("seek must update the progress bar, got %d calls", h.conn().count())
	}

	// A new track is shown.
	h.clk.Advance(5 * time.Second)
	h.tr.Put(song("a", "Two", StatePlaying, 0, 180))
	h.step()
	if h.conn().count() != 3 || h.conn().last().Details != "Two" {
		t.Errorf("new track not shown: %d calls", h.conn().count())
	}
}

func TestEngineThrottlesRapidChanges(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.tr.Put(song("a", "One", StatePlaying, 0, 100))
	h.step()
	h.clk.Advance(500 * time.Millisecond)
	h.tr.Put(song("a", "Two", StatePlaying, 0, 100))
	h.step()
	if h.conn().count() != 1 {
		t.Fatalf("second update within 2s must wait, got %d", h.conn().count())
	}
	h.clk.Advance(2 * time.Second)
	h.tr.Put(song("a", "Two", StatePlaying, 2, 100))
	h.step()
	if h.conn().count() != 2 || h.conn().last().Details != "Two" {
		t.Errorf("deferred update must go out later, got %d", h.conn().count())
	}
}

func TestEnginePauseThenClear(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()
	h.clk.Advance(3 * time.Second)
	h.tr.Put(song("a", "One", StatePaused, 13, 200))
	h.step()
	if last := h.conn().last(); last == nil || last.Timestamps != nil || last.Assets.SmallImage != assetPause {
		t.Fatalf("paused card expected: %+v", last)
	}

	// Keep the source alive with pause heartbeats until the timeout passes.
	for i := 0; i < 12; i++ {
		h.clk.Advance(30 * time.Second)
		h.tr.Put(song("a", "One", StatePaused, 13, 200))
		h.step()
	}
	if h.conn().last() != nil {
		t.Errorf("presence must be cleared after the pause timeout, still showing %+v", h.conn().last())
	}
	if h.eng.Snapshot().PresenceActive {
		t.Error("PresenceActive must be false after clearing")
	}
}

func TestEngineClearsWhenSourceVanishes(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()
	h.clk.Advance(sourceTTL + 5*time.Second)
	h.step()
	if h.conn().count() != 2 || h.conn().last() != nil {
		t.Errorf("want a clear after the browser went away, got %d calls, last=%+v", h.conn().count(), h.conn().last())
	}
}

func TestEngineFallsBackToBasicOnAPIError(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.onNew = func(c *fakeConn) {
		c.fail = func(a *discord.Activity) error {
			if a != nil && a.Assets != nil && a.Assets.SmallImage != "" {
				return &discord.APIError{Code: 4000, Message: "nope"}
			}
			return nil
		}
	}
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()
	last := h.conn().last()
	if last == nil || last.Assets == nil || last.Assets.SmallImage != "" {
		t.Fatalf("basic fallback expected, got %+v", last)
	}
	if !h.eng.basic {
		t.Error("engine must stay in basic mode")
	}
	if st := h.eng.Snapshot().Discord; st.LastError != "" || st.State != StateConnected {
		t.Errorf("fallback worked, no error expected: %+v", st)
	}
}

func TestEngineReconnects(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()
	h.conn().Close() // Discord was closed

	h.clk.Advance(time.Second)
	h.step() // notices the loss and reconnects immediately
	if h.dials != 2 {
		t.Fatalf("want a reconnect, dials=%d", h.dials)
	}
	if h.conn().count() != 1 || h.conn().last().Details != "One" {
		t.Errorf("presence must be restored on the new connection, got %d calls", h.conn().count())
	}
}

func TestEngineBacksOffWhileDiscordIsDown(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.dialErr = errors.New("Discord is not running")
	h.step()
	h.step()
	if h.dials != 1 {
		t.Fatalf("must not hammer Discord, dials=%d", h.dials)
	}
	if st := h.eng.Snapshot().Discord; st.State != StateDisconnected || st.LastError == "" {
		t.Errorf("status should explain: %+v", st)
	}
	h.clk.Advance(4 * time.Second)
	h.step()
	if h.dials != 2 {
		t.Errorf("must retry after the backoff, dials=%d", h.dials)
	}
	h.dialErr = nil
	h.clk.Advance(20 * time.Second)
	h.step()
	if h.eng.Snapshot().Discord.State != StateConnected {
		t.Error("must connect once Discord is back")
	}
}

func TestEngineClientIDChangeReconnects(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.step()
	c := testConfig()
	c.ClientID = "999999999999999999"
	if err := h.cfg.Set(c); err != nil {
		t.Fatal(err)
	}
	h.step()
	if h.dials != 2 {
		t.Errorf("a new application ID needs a new connection, dials=%d", h.dials)
	}
}

func TestInjectTest(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.eng.InjectTest()
	h.step()
	if h.conn().last() == nil || h.conn().last().Details != "Test song" {
		t.Errorf("self-test song not shown: %+v", h.conn().last())
	}
	h.clk.Advance(testTTL + 3*time.Second)
	h.step()
	if h.conn().last() != nil {
		t.Error("self-test must disappear on its own")
	}
}

// ---- switches and extra settings ---------------------------------------------

func TestBuildProgressAndIconSettings(t *testing.T) {
	now := newClock().t
	sel := Selected{Update: song("a", "Song", StatePlaying, 30, 200), ReceivedAt: now}

	cfg := config.Default()
	cfg.ShowProgress = false
	act, start := BuildActivity(cfg, sel, now, false)
	if act.Timestamps != nil || start != 0 {
		t.Errorf("ShowProgress=false must send no timestamps: %+v start=%d", act.Timestamps, start)
	}

	cfg = config.Default()
	cfg.ShowStateIcon = false
	act, _ = BuildActivity(cfg, sel, now, false)
	if act.Assets.SmallImage != "" || act.Assets.SmallText != "" || act.Assets.LargeImage == "" {
		t.Errorf("ShowStateIcon=false must drop only the small badge: %+v", act.Assets)
	}
}

func TestBuildHidesMatchingWords(t *testing.T) {
	now := newClock().t
	sel := Selected{Update: song("a", "A Very Secret Song", StatePlaying, 0, 100), ReceivedAt: now}

	cfg := config.Default()
	cfg.HideMatching = []string{"nothing here", "SECRET"} // capitalisation is ignored
	if act, _ := BuildActivity(cfg, sel, now, false); act != nil {
		t.Error("a title containing a listed word must not be shown")
	}
	sel.Title, sel.Artist = "Fine", "The Secret Band"
	if act, _ := BuildActivity(cfg, sel, now, false); act != nil {
		t.Error("the artist is checked too")
	}
	sel.Artist, sel.Album = "Fine", "Secret Album"
	if act, _ := BuildActivity(cfg, sel, now, false); act != nil {
		t.Error("the album is checked too")
	}
	sel.Album = "Fine"
	if act, _ := BuildActivity(cfg, sel, now, false); act == nil {
		t.Error("no match, so it must be shown")
	}
}

func TestBuildPrivateMode(t *testing.T) {
	now := newClock().t
	sel := Selected{Update: song("a", "My Guilty Pleasure", StatePlaying, 30, 200), ReceivedAt: now}
	cfg := config.Default()
	cfg.PrivateMode = true
	cfg.StatusDisplay = config.StatusDetails

	act, start := BuildActivity(cfg, sel, now, false)
	if act == nil {
		t.Fatal("private mode still shows a card")
	}
	if start != 0 || act.Timestamps != nil || len(act.Buttons) != 0 || act.State != "" || act.StatusDisplayType != nil {
		t.Errorf("private mode must not reveal timing, a link or status text: %+v", act)
	}
	if act.Assets == nil || act.Assets.LargeImage != assetLogo || strings.Contains(act.Assets.LargeText, "Album") {
		t.Errorf("private mode must use the logo, not the album art: %+v", act.Assets)
	}
	raw := act.Details + act.State + act.Assets.LargeImage + act.Assets.LargeText
	for _, secret := range []string{"Guilty", "Artist", "Album", "googleusercontent"} {
		if strings.Contains(raw, secret) {
			t.Errorf("private card leaks %q: %+v", secret, act)
		}
	}

	// Changing song changes nothing, so no update is sent for it.
	sel2 := Selected{Update: song("a", "Another Song", StatePlaying, 5, 100), ReceivedAt: now}
	act2, _ := BuildActivity(cfg, sel2, now, false)
	if activityKey(act) != activityKey(act2) {
		t.Error("in private mode every song must look the same")
	}
	// The pause timeout still applies.
	paused := Selected{Update: song("a", "S", StatePaused, 0, 0), ReceivedAt: now, PausedSince: now}
	if a, _ := BuildActivity(cfg, paused, now.Add(time.Hour), false); a != nil {
		t.Error("a long pause still hides the private card")
	}
}

func TestEngineMasterSwitch(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.step()
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()
	if h.conn().last() == nil || !h.eng.Snapshot().PresenceActive {
		t.Fatal("expected the song to be shown first")
	}

	c := h.cfg.Get()
	c.Enabled = false
	_ = h.cfg.Set(c)
	h.clk.Advance(5 * time.Second)
	h.tr.Put(song("a", "One", StatePlaying, 15, 200))
	h.step()
	snap := h.eng.Snapshot()
	if h.conn().last() != nil || snap.PresenceActive {
		t.Fatalf("switching off must clear the card: last=%v active=%v", h.conn().last(), snap.PresenceActive)
	}
	if snap.Hidden != HiddenOff || snap.NowPlaying == nil {
		t.Errorf("snapshot must say why: hidden=%q np=%v", snap.Hidden, snap.NowPlaying)
	}

	// While off, a new song still doesn't appear, and nothing is sent.
	n := h.conn().count()
	h.clk.Advance(5 * time.Second)
	h.tr.Put(song("a", "Two", StatePlaying, 0, 100))
	h.step()
	if h.conn().count() != n {
		t.Error("nothing may be sent while switched off")
	}

	c.Enabled = true
	_ = h.cfg.Set(c)
	h.clk.Advance(5 * time.Second)
	h.step()
	if h.conn().last() == nil || h.conn().last().Details != "Two" || h.eng.Snapshot().Hidden != "" {
		t.Errorf("switching on must show the current song: %+v", h.conn().last())
	}
}

func TestEngineTimedHide(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	h.step()
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()

	h.eng.HideFor(30 * time.Minute)
	h.clk.Advance(5 * time.Second)
	h.tr.Put(song("a", "One", StatePlaying, 15, 200))
	h.step()
	snap := h.eng.Snapshot()
	if h.conn().last() != nil || snap.Hidden != HiddenSnoozed || snap.SnoozeSec < 29*60 || snap.SnoozeSec > 30*60 {
		t.Fatalf("hide for 30 minutes: last=%v hidden=%q sec=%d", h.conn().last(), snap.Hidden, snap.SnoozeSec)
	}

	// Ends by itself.
	h.clk.Advance(31 * time.Minute)
	h.tr.Put(song("a", "One", StatePlaying, 31*60, 3600))
	h.step()
	if h.conn().last() == nil || h.eng.Snapshot().SnoozeSec != 0 {
		t.Error("the song must come back when the time is up")
	}

	// And can be ended early.
	h.eng.HideFor(time.Hour)
	h.clk.Advance(5 * time.Second)
	h.tr.Put(song("a", "One", StatePlaying, 31*60+5, 3600))
	h.step()
	if h.conn().last() != nil {
		t.Fatal("hidden again")
	}
	h.eng.CancelHide()
	h.clk.Advance(5 * time.Second)
	h.step()
	if h.conn().last() == nil {
		t.Error("CancelHide must bring it back")
	}
}

func TestSnapshotExplainsSettingsHiding(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	cfg := testConfig()
	cfg.HideMatching = []string{"artist"}
	h := newHarness(cfg)
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	if got := h.eng.Snapshot().Hidden; got != HiddenFiltered {
		t.Errorf("want %q, got %q", HiddenFiltered, got)
	}
	cfg.HideMatching = nil
	cfg.ShowPaused = false
	_ = h.cfg.Set(cfg)
	h.tr.Put(song("a", "One", StatePaused, 10, 200))
	if got := h.eng.Snapshot().Hidden; got != HiddenPaused {
		t.Errorf("want %q, got %q", HiddenPaused, got)
	}
}

// A paused tab in the background is slowed by the browser to about one report
// a minute. Its card must stay put between reports, not flicker off and on.
func TestThrottledBackgroundTabDoesNotFlicker(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	cfg := testConfig()
	cfg.PauseTimeoutMinutes = 30
	h := newHarness(cfg)
	h.step()

	h.tr.Put(song("a", "Paused Song", StatePaused, 40, 200))
	h.step()
	if h.conn().last() == nil {
		t.Fatal("the paused card should be showing")
	}
	n := h.conn().count()

	for i := 0; i < 3; i++ { // three minutes of one report per minute, with a little jitter
		for s := 0; s < 62; s += 2 {
			h.clk.Advance(2 * time.Second)
			h.step()
		}
		if h.conn().count() != n || h.conn().last() == nil {
			t.Fatalf("minute %d: the card was cleared between reports (%d sends, was %d)", i+1, h.conn().count(), n)
		}
		h.tr.Put(song("a", "Paused Song", StatePaused, 40, 200))
		h.step()
	}

	// A tab that really is gone (browser killed) still clears, just not instantly.
	h.clk.Advance(QuietAfter() + 5*time.Second)
	h.step()
	if h.conn().last() != nil {
		t.Error("a source that stopped reporting for good must still clear the card")
	}
}

func TestQuietAfterIsOverOneMinute(t *testing.T) {
	if QuietAfter() <= 65*time.Second {
		t.Errorf("a throttled tab reports about every 60s, so %v would flag it as gone", QuietAfter())
	}
}

// ---- show when paused --------------------------------------------------------

func TestShowWhenPaused(t *testing.T) {
	clk := newClock()
	sel := Selected{Update: song("a", "Song", StatePaused, 30, 200), ReceivedAt: clk.t, PausedSince: clk.t}

	// On: the song stays, with the pause icon and no progress bar.
	cfg := config.Default()
	act, _ := BuildActivity(cfg, sel, clk.t.Add(time.Minute), false)
	if act == nil || act.Details != "Song" || act.Timestamps != nil || act.Assets.SmallImage != assetPause || act.Assets.SmallText != "Paused" {
		t.Errorf("paused card should show the song with a pause icon: %+v", act)
	}

	// Off: nothing at all.
	cfg.ShowPaused = false
	if act, _ := BuildActivity(cfg, sel, clk.t, false); act != nil {
		t.Errorf("Show when paused off must hide the card completely: %+v", act)
	}
}

func TestPauseTimeoutZeroNeverHides(t *testing.T) {
	clk := newClock()
	sel := Selected{Update: song("a", "Song", StatePaused, 30, 200), ReceivedAt: clk.t, PausedSince: clk.t}
	cfg := config.Default()

	cfg.PauseTimeoutMinutes = 0
	if act, _ := BuildActivity(cfg, sel, clk.t.Add(48*time.Hour), false); act == nil {
		t.Error("0 minutes means no limit: the paused song must stay")
	}
	cfg.PauseTimeoutMinutes = 10
	if act, _ := BuildActivity(cfg, sel, clk.t.Add(11*time.Minute), false); act != nil {
		t.Error("a positive limit still hides a long pause")
	}
	if act, _ := BuildActivity(cfg, sel, clk.t.Add(9*time.Minute), false); act == nil {
		t.Error("and not before it")
	}
}

// Images for the logo and the play / pause badge must be web addresses: a bare
// key only works once someone uploads art to the Discord application.
func TestBadgeImagesAreWebAddresses(t *testing.T) {
	now := newClock().t
	for _, state := range []string{StatePlaying, StatePaused} {
		sel := Selected{Update: song("a", "Song", state, 5, 100), ReceivedAt: now, PausedSince: now}
		sel.Artwork = "" // so the logo is used too
		act, _ := BuildActivity(config.Default(), sel, now, false)
		for _, img := range []string{act.Assets.LargeImage, act.Assets.SmallImage} {
			if !strings.HasPrefix(img, "https://raw.githubusercontent.com/dash1101/ytm-beacon/main/assets/") || !strings.HasSuffix(img, ".png") {
				t.Errorf("%s: %q is not a web address of an image in assets/", state, img)
			}
		}
	}
}

// The addresses above point at these files in the repository, so renaming or
// deleting one would silently break the icons.
func TestBadgeImageFilesExist(t *testing.T) {
	for _, img := range []string{assetLogo, assetPlay, assetPause} {
		name := img[strings.LastIndex(img, "/")+1:]
		if _, err := os.Stat("../../assets/" + name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Two tabs both playing (or a tab and a forgotten one) report every few
// seconds in turn. The card must stay on the one where play was pressed last
// instead of flipping with every report.
func TestTrackerDoesNotFlipBetweenTwoPlayingTabs(t *testing.T) {
	clk := newClock()
	tr := NewTrackerWithClock(clk.Now)
	tr.Put(song("a", "A", StatePlaying, 0, 0))
	clk.Advance(time.Second)
	tr.Put(song("b", "B", StatePlaying, 0, 0)) // play pressed in b last
	for i := 0; i < 6; i++ {
		clk.Advance(2 * time.Second)
		src := "a"
		if i%2 == 1 {
			src = "b"
		}
		tr.Put(song(src, strings.ToUpper(src), StatePlaying, float64(i), 0)) // heartbeats in turn
		if sel, _ := tr.Select(); sel.Title != "B" {
			t.Fatalf("report %d from %s flipped the card to %s", i, src, sel.Title)
		}
	}
	// Pausing b and pressing play in a again moves it to a.
	tr.Put(song("b", "B", StatePaused, 0, 0))
	if sel, _ := tr.Select(); sel.Title != "A" {
		t.Errorf("the tab still playing must win, got %s", sel.Title)
	}
	clk.Advance(time.Second)
	tr.Put(song("b", "B", StatePlaying, 0, 0))
	if sel, _ := tr.Select(); sel.Title != "B" {
		t.Errorf("pressing play again must bring b back, got %s", sel.Title)
	}
}

func TestTrackerPicksTheTabPausedLast(t *testing.T) {
	clk := newClock()
	tr := NewTrackerWithClock(clk.Now)
	tr.Put(song("a", "A", StatePaused, 0, 0))
	clk.Advance(time.Second)
	tr.Put(song("b", "B", StatePaused, 0, 0))
	clk.Advance(30 * time.Second)
	tr.Put(song("a", "A", StatePaused, 0, 0)) // "still paused" report from the older one
	if sel, _ := tr.Select(); sel.Title != "B" {
		t.Errorf("the tab paused last must win, got %s", sel.Title)
	}
}

// After falling back to the plain card for an older Discord, a new connection
// (Discord restarted, maybe updated) tries the full card again.
func TestEngineRetriesFullCardAfterReconnect(t *testing.T) {
	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	h := newHarness(testConfig())
	reject := true
	h.onNew = func(c *fakeConn) {
		c.fail = func(a *discord.Activity) error {
			if reject && a != nil && a.Assets != nil && a.Assets.SmallImage != "" {
				return &discord.APIError{Code: 4000, Message: "nope"}
			}
			return nil
		}
	}
	h.tr.Put(song("a", "One", StatePlaying, 10, 200))
	h.step()
	if !h.eng.basic {
		t.Fatal("expected the fallback on the old Discord")
	}
	reject = false
	h.conn().Close()
	h.clk.Advance(time.Second)
	h.step()
	if h.eng.basic {
		t.Error("a new connection must try the full card again")
	}
	if last := h.conn().last(); last == nil || last.Assets == nil || last.Assets.SmallImage == "" {
		t.Errorf("want the full card on the new connection, got %+v", last)
	}
}

func TestAssetsPointAtTheReleaseTag(t *testing.T) {
	cases := map[string]string{
		"v0.1.5":     "https://raw.githubusercontent.com/dash1101/ytm-beacon/v0.1.5/assets/",
		"v1.2.3-rc1": "https://raw.githubusercontent.com/dash1101/ytm-beacon/v1.2.3-rc1/assets/",
		"dev":        "https://raw.githubusercontent.com/dash1101/ytm-beacon/main/assets/",
		"ci":         "https://raw.githubusercontent.com/dash1101/ytm-beacon/main/assets/",
		"0.1.5":      "https://raw.githubusercontent.com/dash1101/ytm-beacon/main/assets/", // tags have a v
		"v1/../x":    "https://raw.githubusercontent.com/dash1101/ytm-beacon/main/assets/",
	}
	for ver, want := range cases {
		if got := assetBaseFor(ver); got != want {
			t.Errorf("%q: got %q, want %q", ver, got, want)
		}
	}
}
