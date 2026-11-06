package presence

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
)

// The logo and the play / pause badge are sent as web addresses, served from
// this repository's assets/ folder. Discord fetches them itself, so nobody has
// to upload art to a Discord application (an image key that isn't uploaded just
// shows nothing, which is how the pause icon used to go missing).
const assetBase = "https://raw.githubusercontent.com/dash1101/ytm-beacon/main/assets/"

const (
	assetLogo  = assetBase + "logo.png"
	assetPlay  = assetBase + "play.png"
	assetPause = assetBase + "pause.png"
)

// field fits text into what Discord accepts (2-128 characters).
func field(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if r := []rune(s); len(r) > 100 {
		s = string(r[:99]) + "…"
	}
	if utf8.RuneCountInString(s) < 2 {
		s += "​" // zero-width space: Discord rejects one-character fields
	}
	return s
}

func trackURL(sel Selected) string {
	if sel.VideoID != "" {
		return "https://music.youtube.com/watch?v=" + sel.VideoID
	}
	q := truncate(sel.Artist+" "+sel.Title, 120)
	return "https://music.youtube.com/search?q=" + url.QueryEscape(q)
}

// Why a song that is playing or paused is not on Discord. The engine adds
// HiddenOff and HiddenSnoozed (the user's own switches); the rest come from
// the settings. The dashboard and popup turn these into plain sentences.
const (
	HiddenOff           = "off"            // the master switch is off
	HiddenSnoozed       = "snoozed"        // hidden for a while on request
	HiddenPaused        = "paused"         // paused, and settings say not to show that
	HiddenPausedTimeout = "paused-timeout" // paused for longer than the limit
	HiddenFiltered      = "filtered"       // matched a "never show" word
)

// hiddenReason says why the settings keep this song off Discord ("" = show it).
func hiddenReason(cfg config.Config, sel Selected, now time.Time) string {
	if matchesHidden(cfg.HideMatching, sel) {
		return HiddenFiltered
	}
	if sel.State == StatePaused {
		if !cfg.ShowPaused {
			return HiddenPaused
		}
		if limit := cfg.PauseTimeoutMinutes; limit > 0 && now.Sub(sel.PausedSince) > time.Duration(limit)*time.Minute {
			return HiddenPausedTimeout
		}
	}
	return ""
}

func matchesHidden(words []string, sel Selected) bool {
	if len(words) == 0 {
		return false
	}
	hay := strings.ToLower(sel.Title + "\n" + sel.Artist + "\n" + sel.Album)
	for _, w := range words {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" && strings.Contains(hay, w) {
			return true
		}
	}
	return false
}

// BuildActivity turns the selected track into a Discord activity. It returns
// nil when nothing should be shown (for example paused for too long). The
// second result is the start timestamp in milliseconds (0 when none), which
// the engine uses to notice seeks.
//
// basic=true leaves out the newer, optional fields. The engine falls back to it
// if a Discord client rejects the full version.
func BuildActivity(cfg config.Config, sel Selected, now time.Time, basic bool) (*discord.Activity, int64) {
	if hiddenReason(cfg, sel, now) != "" {
		return nil, 0
	}
	paused := sel.State == StatePaused
	if cfg.PrivateMode {
		return buildPrivate(cfg, paused, basic), 0
	}

	act := &discord.Activity{
		Type:    discord.TypeListening,
		Details: field(sel.Title),
		State:   field(sel.Artist),
	}

	var start int64
	if !paused && cfg.ShowProgress {
		start = sel.ReceivedAt.Add(-time.Duration(sel.Position * float64(time.Second))).UnixMilli()
		ts := &discord.Timestamps{Start: start}
		if sel.Duration > 0 && sel.Position <= sel.Duration+1 {
			ts.End = start + int64(sel.Duration*1000)
		}
		act.Timestamps = ts
	}

	assets := &discord.Assets{}
	switch {
	case cfg.ShowAlbumArt && sel.Artwork != "":
		assets.LargeImage = sel.Artwork
	case !basic:
		assets.LargeImage = assetLogo
	}
	if assets.LargeImage != "" {
		text := sel.Album
		if text == "" {
			text = sel.Title
		}
		assets.LargeText = field(text)
	}
	addStateIcon(cfg, assets, paused, basic)
	if *assets != (discord.Assets{}) {
		act.Assets = assets
	}

	if cfg.ShowButtons {
		act.Buttons = []discord.Button{{Label: "Listen on YouTube Music", URL: trackURL(sel)}}
	}

	if !basic {
		switch cfg.StatusDisplay {
		case config.StatusState:
			v := discord.StatusDisplayState
			act.StatusDisplayType = &v
		case config.StatusDetails:
			v := discord.StatusDisplayDetails
			act.StatusDisplayType = &v
		}
	}
	return act, start
}

// addStateIcon adds the small play / pause badge.
func addStateIcon(cfg config.Config, assets *discord.Assets, paused, basic bool) {
	if basic || !cfg.ShowStateIcon {
		return
	}
	if paused {
		assets.SmallImage, assets.SmallText = assetPause, "Paused"
	} else {
		assets.SmallImage, assets.SmallText = assetPlay, "Playing"
	}
}

// buildPrivate is the card for private mode: it says you're listening and
// nothing about what to. It is the same for every song, so changing tracks
// does not even cause an update.
func buildPrivate(cfg config.Config, paused, basic bool) *discord.Activity {
	act := &discord.Activity{Type: discord.TypeListening, Details: "Listening to music"}
	assets := &discord.Assets{}
	if !basic {
		assets.LargeImage = assetLogo
	}
	addStateIcon(cfg, assets, paused, basic)
	if *assets != (discord.Assets{}) {
		act.Assets = assets
	}
	return act
}
