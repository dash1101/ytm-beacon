// Package config loads and saves the user's settings (a small JSON file).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// DefaultClientID is the Discord application the released builds use. It is
// public information (not a secret). Users can override it in config.json or
// with the YTM_BEACON_CLIENT_ID environment variable.
var DefaultClientID = "1567329407901634670"

// Status display choices decide what Discord shows next to your name in the
// member list: "Listening to <app name>", <artist> or <song title>.
const (
	StatusName    = "name"
	StatusState   = "state"
	StatusDetails = "details"
)

// Limits for the "never show" word list, so a pasted novel can't bloat the file.
const (
	maxHideWords   = 50
	maxHideWordLen = 80
)

type Config struct {
	// Enabled is the master switch. When false nothing is shown on Discord (the
	// app keeps running, so turning it back on is instant).
	Enabled bool `json:"enabled"`
	// ClientID is the Discord application ID. Empty means "use the built-in one".
	ClientID string `json:"clientId"`
	// ShowButtons adds a "Listen on YouTube Music" button to your profile card.
	ShowButtons bool `json:"showButtons"`
	// ShowPaused keeps the song on your profile while paused (with a pause icon)
	// instead of hiding the card the moment you pause.
	ShowPaused bool `json:"showPaused"`
	// PauseTimeoutMinutes hides a paused card after this long. 0 means never:
	// the song stays until you press play again.
	PauseTimeoutMinutes int `json:"pauseTimeoutMinutes"`
	// ShowAlbumArt shows the track's artwork instead of the app logo.
	ShowAlbumArt bool `json:"showAlbumArt"`
	// StatusDisplay is one of StatusName, StatusState, StatusDetails.
	StatusDisplay string `json:"statusDisplay"`
	// ShowProgress shows the progress bar and elapsed time.
	ShowProgress bool `json:"showProgress"`
	// ShowStateIcon adds the small play / pause badge on the album art.
	ShowStateIcon bool `json:"showStateIcon"`
	// PrivateMode shows only "Listening to music": no song, artist, art or button.
	PrivateMode bool `json:"privateMode"`
	// HideMatching lists words; a song whose title, artist or album contains one
	// of them (any capitalisation) is never shown.
	HideMatching []string `json:"hideMatching"`
}

func Default() Config {
	return Config{
		Enabled:             true,
		ShowButtons:         true,
		ShowPaused:          true,
		PauseTimeoutMinutes: 5,
		ShowAlbumArt:        true,
		StatusDisplay:       StatusName,
		ShowProgress:        true,
		ShowStateIcon:       true,
		HideMatching:        []string{},
	}
}

var clientIDPattern = regexp.MustCompile(`^[0-9]{15,25}$`)

// Normalize validates the config and clamps out-of-range numbers.
func (c *Config) Normalize() error {
	c.ClientID = strings.TrimSpace(c.ClientID)
	if c.ClientID != "" && !clientIDPattern.MatchString(c.ClientID) {
		return errors.New("the Discord application ID should be a long number (17-19 digits), copied from the Discord Developer Portal")
	}
	c.StatusDisplay = strings.ToLower(strings.TrimSpace(c.StatusDisplay))
	if c.StatusDisplay == "" {
		c.StatusDisplay = StatusName
	}
	switch c.StatusDisplay {
	case StatusName, StatusState, StatusDetails:
	default:
		return fmt.Errorf("statusDisplay must be %q, %q or %q", StatusName, StatusState, StatusDetails)
	}
	if c.PauseTimeoutMinutes < 0 {
		c.PauseTimeoutMinutes = 0
	}
	if c.PauseTimeoutMinutes > 24*60 {
		c.PauseTimeoutMinutes = 24 * 60
	}
	c.HideMatching = cleanWords(c.HideMatching)
	return nil
}

// cleanWords trims, lower-cases and de-duplicates the word list, dropping
// blanks and anything beyond the limits. It never returns nil, so the JSON
// file and the API always show a list.
func cleanWords(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, w := range in {
		w = strings.ToLower(strings.TrimSpace(w))
		if r := []rune(w); len(r) > maxHideWordLen {
			w = string(r[:maxHideWordLen])
		}
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		if out = append(out, w); len(out) == maxHideWords {
			break
		}
	}
	return out
}

// EffectiveClientID picks the application ID to use: environment variable,
// then config file, then the built-in default.
func (c Config) EffectiveClientID() string {
	if v := strings.TrimSpace(os.Getenv("YTM_BEACON_CLIENT_ID")); v != "" {
		return v
	}
	if c.ClientID != "" {
		return c.ClientID
	}
	return DefaultClientID
}

// Store is a Config that can be read and changed from several goroutines.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// NewMemoryStore returns a Store that never touches the disk (for tests).
func NewMemoryStore(c Config) *Store { return &Store{cfg: c} }

// Load reads the config file. A missing file is fine (defaults are used). If the
// file is broken, the returned Store still works with defaults and the error
// says what is wrong, so the app can warn and carry on.
func Load(path string) (*Store, error) {
	s := &Store{path: path, cfg: Default()}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("cannot read %s: %w", path, err)
	}
	c := Default()
	if err := json.Unmarshal(b, &c); err != nil {
		return s, fmt.Errorf("%s is not valid JSON (%v); using defaults", path, err)
	}
	if err := c.Normalize(); err != nil {
		return s, fmt.Errorf("%s: %v; using defaults", path, err)
	}
	s.cfg = c
	return s, nil
}

func (s *Store) Path() string { return s.path }

// Get returns a copy of the settings; changing it does not change the Store.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.cfg
	c.HideMatching = append([]string{}, c.HideMatching...)
	return c
}

// Set validates, stores and saves a new config.
func (s *Store) Set(c Config) error {
	if err := c.Normalize(); err != nil {
		return err
	}
	if s.path != "" {
		if err := save(s.path, c); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.cfg = c
	s.mu.Unlock()
	return nil
}

func save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
