//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dash1101/ytm-beacon/internal/paths"
)

func plistPath() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "Library", "LaunchAgents", Label+".plist"), nil
}

func install(exe string) (string, error) {
	pp, err := plistPath()
	if err != nil {
		return "", err
	}
	data, err := paths.DataDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(pp), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(pp, []byte(launchdPlist(Label, exe, filepath.Join(data, "launchd.log"))), 0o644); err != nil {
		return "", err
	}

	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+Label).Run() // ignore: may not be loaded
	time.Sleep(300 * time.Millisecond)
	if out, err := exec.Command("launchctl", "bootstrap", domain, pp).CombinedOutput(); err != nil {
		// Older macOS versions only know the legacy commands.
		_ = exec.Command("launchctl", "unload", pp).Run()
		if out2, err2 := exec.Command("launchctl", "load", "-w", pp).CombinedOutput(); err2 != nil {
			return "", fmt.Errorf("launchctl failed: %v: %s / %s", err2, out, out2)
		}
	}
	return "launch agent (" + pp + ")", nil
}

func uninstall() error {
	pp, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+Label).Run()
	_ = exec.Command("launchctl", "unload", pp).Run()
	_ = os.Remove(pp)
	return nil
}

func installed() (bool, string) {
	pp, err := plistPath()
	if err != nil {
		return false, ""
	}
	if _, err := os.Stat(pp); err == nil {
		return true, "launch agent"
	}
	return false, ""
}
