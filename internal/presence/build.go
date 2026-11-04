package presence

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
)

// Asset keys. Upload assets/logo.png, play.png and pause.png to the Discord
// application under these names (Developer Portal > Rich Presence > Art Assets).
// If they are missing Discord simply shows no image there.
const (
	assetLogo  = "logo"
	assetPlay  = "play"
	assetPause = "pause"
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

// BuildActivity turns the selected track into a Discord activity. It returns
// nil when nothing should be shown (for example paused for too long). The
// second result is the start timestamp in milliseconds (0 when none), which
// the engine uses to notice seeks.
//
// basic=true leaves out the newer, optional fields. The engine falls back to it
// if a Discord client rejects the full version.
func BuildActivity(cfg config.Config, sel Selected, now time.Time, basic bool) (*discord.Activity, int64) {
	paused := sel.State == StatePaused
	if paused {
		if !cfg.ShowPaused {
			return nil, 0
		}
		if now.Sub(sel.PausedSince) > time.Duration(cfg.PauseTimeoutMinutes)*time.Minute {
			return nil, 0
		}
	}

	act := &discord.Activity{
		Type:    discord.TypeListening,
		Details: field(sel.Title),
		State:   field(sel.Artist),
	}

	var start int64
	if !paused {
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
	if !basic {
		if paused {
			assets.SmallImage, assets.SmallText = assetPause, "Paused"
		} else {
			assets.SmallImage, assets.SmallText = assetPlay, "Playing"
		}
	}
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
