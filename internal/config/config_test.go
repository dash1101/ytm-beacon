package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMissingFileGivesDefaults(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Get(), Default()) {
		t.Errorf("got %+v", s.Get())
	}
}

func TestSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	s, _ := Load(path)
	c := s.Get()
	c.ClientID = " 123456789012345678 "
	c.ShowButtons = false
	c.PauseTimeoutMinutes = 0 // clamped
	if err := s.Set(c); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Get()
	if got.ClientID != "123456789012345678" || got.ShowButtons || got.PauseTimeoutMinutes != 1 {
		t.Errorf("not persisted correctly: %+v", got)
	}
}

func TestPartialFileKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"showButtons": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Get(); c.ShowButtons || !c.ShowPaused || !c.ShowAlbumArt || c.PauseTimeoutMinutes != 5 {
		t.Errorf("unset fields must keep defaults: %+v", c)
	}
	// A settings file written by an older version has none of the newer fields;
	// they must come out switched on (or empty), not silently off.
	if c := s.Get(); !c.Enabled || !c.ShowProgress || !c.ShowStateIcon || c.PrivateMode || len(c.HideMatching) != 0 {
		t.Errorf("newer settings must default sensibly: %+v", c)
	}
}

func TestHideMatchingIsCleaned(t *testing.T) {
	c := Default()
	c.HideMatching = []string{"  Taylor Swift ", "", "taylor swift", "LIVE", "   "}
	for i := 0; i < 80; i++ {
		c.HideMatching = append(c.HideMatching, "word"+strings.Repeat("x", i))
	}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if got := c.HideMatching; len(got) != maxHideWords || got[0] != "taylor swift" || got[1] != "live" {
		t.Errorf("not cleaned: %d words, starts %q", len(got), got[:2])
	}
	c.HideMatching = nil
	_ = c.Normalize()
	if c.HideMatching == nil {
		t.Error("an empty list must stay a list (not null) so the API and file show []")
	}
}

func TestGetReturnsACopy(t *testing.T) {
	s := NewMemoryStore(Default())
	c := Default()
	c.HideMatching = []string{"a", "b"}
	if err := s.Set(c); err != nil {
		t.Fatal(err)
	}
	c.HideMatching[0] = "changed by the caller"
	got := s.Get()
	got.HideMatching[0] = "changed"
	if s.Get().HideMatching[0] != "a" {
		t.Error("editing the copy returned by Get must not change the stored settings")
	}
}

func TestBrokenFileFallsBackWithError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, content := range []string{`{not json`, `{"clientId":"abc"}`, `{"statusDisplay":"sideways"}`} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		s, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%q: want an error naming the file, got %v", content, err)
		}
		if !reflect.DeepEqual(s.Get(), Default()) {
			t.Errorf("%q: must fall back to defaults", content)
		}
	}
}

func TestEffectiveClientID(t *testing.T) {
	old := DefaultClientID
	defer func() { DefaultClientID = old }()
	DefaultClientID = "111111111111111111"

	t.Setenv("YTM_BEACON_CLIENT_ID", "")
	c := Default()
	if c.EffectiveClientID() != "111111111111111111" {
		t.Error("built-in default expected")
	}
	c.ClientID = "222222222222222222"
	if c.EffectiveClientID() != "222222222222222222" {
		t.Error("config value must beat the built-in one")
	}
	t.Setenv("YTM_BEACON_CLIENT_ID", "333333333333333333")
	if c.EffectiveClientID() != "333333333333333333" {
		t.Error("environment must win")
	}
}
