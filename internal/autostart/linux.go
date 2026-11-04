//go:build linux

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func configDir() (string, error) { return os.UserConfigDir() }

func unitPath() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "systemd", "user", ServiceName+".service"), nil
}

func desktopPath() (string, error) {
	d, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "autostart", ServiceName+".desktop"), nil
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func systemdAvailable() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exec.Command("systemctl", "--user", "show-environment").Run() == nil
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %v: %v: %s", args, err, out)
	}
	return nil
}

func install(exe string) (string, error) {
	if systemdAvailable() {
		up, err := unitPath()
		if err == nil {
			err = writeFile(up, systemdUnit(exe))
		}
		if err == nil {
			err = systemctl("daemon-reload")
		}
		if err == nil {
			err = systemctl("enable", ServiceName+".service")
		}
		if err == nil {
			err = systemctl("restart", ServiceName+".service")
		}
		if err == nil {
			if dp, e := desktopPath(); e == nil {
				_ = os.Remove(dp) // avoid starting twice
			}
			return "systemd user service (" + ServiceName + ".service)", nil
		}
		// fall through to the plain autostart entry
	}

	dp, err := desktopPath()
	if err != nil {
		return "", err
	}
	if err := writeFile(dp, desktopEntry(exe)); err != nil {
		return "", err
	}
	if err := startDetached(exe, "run"); err != nil {
		return "", err
	}
	return "autostart entry (" + dp + ")", nil
}

func uninstall() error {
	if systemdAvailable() {
		_ = systemctl("disable", "--now", ServiceName+".service")
	}
	if up, err := unitPath(); err == nil {
		_ = os.Remove(up)
	}
	if dp, err := desktopPath(); err == nil {
		_ = os.Remove(dp)
	}
	if systemdAvailable() {
		_ = systemctl("daemon-reload")
	}
	return nil
}

func installed() (bool, string) {
	if up, err := unitPath(); err == nil {
		if _, err := os.Stat(up); err == nil {
			return true, "systemd user service"
		}
	}
	if dp, err := desktopPath(); err == nil {
		if _, err := os.Stat(dp); err == nil {
			return true, "autostart entry"
		}
	}
	return false, ""
}
