//go:build !windows

package main

// ownsConsole only matters on Windows (see console_windows.go).
func ownsConsole() bool { return false }
