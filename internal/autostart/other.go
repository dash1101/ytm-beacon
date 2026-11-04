//go:build !linux && !darwin && !windows

package autostart

import "errors"

func install(string) (string, error) {
	return "", errors.New("start-at-login is not supported on this operating system; run `ytm-beacon run` yourself")
}

func uninstall() error { return nil }

func installed() (bool, string) { return false, "" }
