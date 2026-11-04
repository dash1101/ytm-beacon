//go:build windows

package installer

import (
	"os/exec"
	"syscall"
)

// scheduleDelete removes a file a moment after this program exits. Windows
// won't let a running program delete its own file, so a hidden helper waits a
// couple of seconds and then does it.
func scheduleDelete(path string) {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       `cmd /c ping -n 3 127.0.0.1 >nul & del /f /q "` + path + `"`,
		HideWindow:    true,
		CreationFlags: 0x00000008, // DETACHED_PROCESS
	}
	_ = cmd.Start()
}
