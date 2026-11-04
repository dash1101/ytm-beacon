//go:build !windows

package autostart

import (
	"os/exec"
	"syscall"
)

// startDetached starts a program that keeps running after we exit.
func startDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
