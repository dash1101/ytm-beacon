//go:build !windows

package discord

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// SearchDirs lists the folders where Discord may keep its IPC socket. Besides
// the usual runtime/temp folders this covers the Flatpak and Snap versions of
// Discord (and Discord-compatible clients such as Vesktop).
func SearchDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}

	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" && runtime.GOOS == "linux" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	add(runtimeDir)
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		add(os.Getenv(k))
	}
	if runtimeDir != "" {
		add(filepath.Join(runtimeDir, "app", "com.discordapp.Discord"))
		add(filepath.Join(runtimeDir, "snap.discord"))
		add(filepath.Join(runtimeDir, "snap.discord-canary"))
		// Other Flatpak apps that offer Discord's socket (Vesktop, Legcord...).
		for _, pattern := range []string{
			filepath.Join(runtimeDir, "app", "*"),
			filepath.Join(runtimeDir, ".flatpak", "*", "xdg-run"),
		} {
			if m, err := filepath.Glob(pattern); err == nil {
				for _, d := range m {
					add(d)
				}
			}
		}
	}
	add(os.TempDir())
	add("/tmp")
	return dirs
}

// Endpoints returns the Discord sockets that currently exist, lowest number first.
func Endpoints() []string {
	var eps []string
	dirs := SearchDirs()
	for i := 0; i < 10; i++ {
		for _, d := range dirs {
			p := filepath.Join(d, fmt.Sprintf("discord-ipc-%d", i))
			if fi, err := os.Stat(p); err == nil && fi.Mode()&os.ModeSocket != 0 {
				eps = append(eps, p)
			}
		}
	}
	return eps
}

func dial(endpoint string) (net.Conn, error) {
	return net.DialTimeout("unix", endpoint, 2*time.Second)
}
