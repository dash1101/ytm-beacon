// Package paths decides where the app keeps its files on each operating system.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AppName is used for folder and file names.
const AppName = "ytm-beacon"

// Setting YTM_BEACON_HOME moves everything under one folder. It exists for
// testing and for portable setups; normal users never need it.
func override() string { return os.Getenv("YTM_BEACON_HOME") }

// ConfigDir holds config.json.
func ConfigDir() (string, error) {
	if h := override(); h != "" {
		return filepath.Join(h, "config"), nil
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot find your config folder: %w", err)
	}
	return filepath.Join(d, AppName), nil
}

// DataDir holds the log file and the unpacked browser extension.
func DataDir() (string, error) {
	if h := override(); h != "" {
		return filepath.Join(h, "data"), nil
	}
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, AppName), nil
		}
	case "darwin":
		d, err := os.UserConfigDir() // ~/Library/Application Support
		if err != nil {
			return "", fmt.Errorf("cannot find your Application Support folder: %w", err)
		}
		return filepath.Join(d, AppName), nil
	default:
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, AppName), nil
		}
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find your home folder: %w", err)
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(h, "AppData", "Local", AppName), nil
	}
	return filepath.Join(h, ".local", "share", AppName), nil
}

// BinDir is where `install` puts the program.
func BinDir() (string, error) {
	if h := override(); h != "" {
		return filepath.Join(h, "bin"), nil
	}
	if runtime.GOOS == "windows" {
		return DataDir()
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find your home folder: %w", err)
	}
	return filepath.Join(h, ".local", "bin"), nil
}

// ExeName adds ".exe" on Windows.
func ExeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func ConfigFile() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

func LogFile() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, AppName+".log"), nil
}

// ConnectedMarker is an empty file created the first time a browser extension
// reports in. Its existence tells the dashboard and installer that setup is
// finished, so they stop showing first-run steps.
func ConnectedMarker() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "extension-connected"), nil
}

// ExtensionDir is where `install` unpacks the extension for the given browser
// family ("chrome" or "firefox").
func ExtensionDir(browser string) (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "extension", browser), nil
}
