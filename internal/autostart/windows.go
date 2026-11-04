//go:build windows

package autostart

import (
	"fmt"
	"os/exec"
)

const (
	runKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "YTMBeacon"
)

func install(exe string) (string, error) {
	cmdline := `"` + exe + `" run`
	out, err := exec.Command("reg", "add", runKey, "/v", runValue, "/t", "REG_SZ", "/d", cmdline, "/f").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("reg add failed: %v: %s", err, out)
	}
	if err := startDetached(exe, "run"); err != nil {
		return "", err
	}
	return "Windows startup entry", nil
}

func uninstall() error {
	_ = exec.Command("reg", "delete", runKey, "/v", runValue, "/f").Run()
	return nil
}

func installed() (bool, string) {
	if exec.Command("reg", "query", runKey, "/v", runValue).Run() == nil {
		return true, "Windows startup entry"
	}
	return false, ""
}
