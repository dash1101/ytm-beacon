// Package autostart makes the app start by itself when you log in, using the
// native mechanism of each OS: a systemd user service (or XDG autostart entry)
// on Linux, a launch agent on macOS and a Run registry entry on Windows.
package autostart

import (
	"errors"
	"fmt"
)

const (
	// ServiceName is used for the systemd unit and the .desktop file.
	ServiceName = "ytm-beacon"
	// Label identifies the macOS launch agent.
	Label = "dev.ytm-beacon"
)

// Install registers exe to start at login (with the "run" argument) and starts
// it now. It returns a short description of what was set up.
func Install(exe string) (string, error) {
	if exe == "" {
		return "", errors.New("no program path given")
	}
	how, err := install(exe)
	if err != nil {
		return "", fmt.Errorf("could not set up start-at-login: %w", err)
	}
	return how, nil
}

// Uninstall removes the start-at-login registration. It does not stop a
// running copy; the caller does that.
func Uninstall() error { return uninstall() }

// Installed reports whether start-at-login is set up, and how.
func Installed() (bool, string) { return installed() }
