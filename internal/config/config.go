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

type Config struct {
	// ClientID is the Discord application ID. Empty means "use the built-in one".
	ClientID string `json:"clientId"`
	// ShowButtons adds a "Listen on YouTube Music" button to your profile card.
	ShowButtons bool `json:"showButtons"`
	// ShowPaused keeps the card visible (marked paused) instead of hiding it.
	ShowPaused bool `json:"showPaused"`
	// PauseTimeoutMinutes hides a paused card after this long.
	PauseTimeoutMinutes int `json:"pauseTimeoutMinutes"`
	// ShowAlbumArt shows the track's artwork instead of the app logo.
	ShowAlbumArt bool `json:"showAlbumArt"`
	// StatusDisplay is one of StatusName, StatusState, StatusDetails.
	StatusDisplay string `json:"statusDisplay"`
}

func Default() Config {
	return Config{
		ShowButtons:         true,
		ShowPaused:          true,
		PauseTimeoutMinutes: 5,
		ShowAlbumArt:        true,
		StatusDisplay:       StatusName,
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
	if c.PauseTimeoutMinutes < 1 {
		c.PauseTimeoutMinutes = 1
	}
	if c.PauseTimeoutMinutes > 24*60 {
		c.PauseTimeoutMinutes = 24 * 60
	}
	return nil
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

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
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
