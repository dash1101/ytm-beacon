// Package installer implements `ytm-beacon install` and `uninstall`: copy the
// program to a permanent place, unpack the browser extension, and set the app
// to start at login.
package installer

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dash1101/ytm-beacon/internal/autostart"
	"github.com/dash1101/ytm-beacon/internal/extension"
	"github.com/dash1101/ytm-beacon/internal/paths"
	"github.com/dash1101/ytm-beacon/internal/server"
	"github.com/dash1101/ytm-beacon/internal/version"
)

// BackgroundExeName is the Windows build without a console window. When it sits
// next to the main program it is what starts at login, so no black window ever
// appears. Other systems don't need it.
const BackgroundExeName = "ytm-beacon-bg.exe"

func installedExe() (string, error) {
	dir, err := paths.BinDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, paths.ExeName("ytm-beacon")), nil
}

// Install does everything. extSrc is the embedded extension (rooted at its manifest files).
func Install(out io.Writer, extSrc fs.FS) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find this program's own path: %w", err)
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	target, err := installedExe()
	if err != nil {
		return err
	}
	binDir := filepath.Dir(target)

	fmt.Fprintf(out, "[1/4] Installing the program to %s\n", binDir)
	if err := server.Quit(); err != nil {
		fmt.Fprintf(out, "      note: could not stop the running copy (%v); continuing\n", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if !sameFile(self, target) {
		if err := copyFile(self, target); err != nil {
			return fmt.Errorf("could not copy the program: %w", err)
		}
	}
	daemon := target
	if runtime.GOOS == "windows" {
		bgSrc := filepath.Join(filepath.Dir(self), BackgroundExeName)
		bgDst := filepath.Join(binDir, BackgroundExeName)
		if _, err := os.Stat(bgSrc); err == nil {
			if !sameFile(bgSrc, bgDst) {
				if err := copyFile(bgSrc, bgDst); err != nil {
					return fmt.Errorf("could not copy the background program: %w", err)
				}
			}
			daemon = bgDst
		} else if _, err := os.Stat(bgDst); err == nil {
			daemon = bgDst
		}
	}
	allowToRun(target)
	pathNote := ensureOnPath(binDir)

	fmt.Fprintln(out, "[2/4] Unpacking the browser extension")
	for _, b := range extension.Browsers {
		dir, err := paths.ExtensionDir(b)
		if err != nil {
			return err
		}
		if err := extension.Write(extSrc, b, version.Version, dir); err != nil {
			return fmt.Errorf("could not unpack the %s extension: %w", b, err)
		}
	}

	fmt.Fprintln(out, "[3/4] Setting it to start when you log in")
	how, err := autostart.Install(daemon)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "      done: %s\n", how)

	fmt.Fprintln(out, "[4/4] Starting it")
	started := false
	for i := 0; i < 40; i++ {
		if _, err := server.FetchStatus(); err == nil {
			started = true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if started {
		fmt.Fprintln(out, "      running")
	} else {
		fmt.Fprintln(out, "      it has not answered yet. Run `ytm-beacon doctor` to see why.")
	}

	chrome, _ := paths.ExtensionDir("chrome")
	firefox, _ := paths.ExtensionDir("firefox")
	fmt.Fprintf(out, `
Installed. One thing left: add the extension to your browser (one time).

  Chrome, Brave, Edge, Opera, Vivaldi:
    1. Open chrome://extensions (brave://extensions, edge://extensions ...)
    2. Turn on "Developer mode"
    3. Click "Load unpacked" and choose this folder:
         %s

  Firefox:
    Install the signed .xpi from the Releases page (permanent), or for a quick
    try: open about:debugging#/runtime/this-firefox, click "Load Temporary
    Add-on..." and choose manifest.json inside:
         %s

Then open https://music.youtube.com and play a song.
Dashboard and settings: http://127.0.0.1:%d/
Something wrong? Run:  ytm-beacon doctor
`, chrome, firefox, server.Ports[0])
	if pathNote != "" {
		fmt.Fprintln(out, "\n"+pathNote)
	}
	return nil
}

// Uninstall removes start-at-login, the extension copy and (best effort) the
// program. With purge it also deletes settings and logs.
func Uninstall(out io.Writer, purge bool) error {
	fmt.Fprintln(out, "Stopping YTM Beacon...")
	if err := server.Quit(); err != nil {
		fmt.Fprintf(out, "  note: %v\n", err)
	}
	fmt.Fprintln(out, "Removing start-at-login...")
	if err := autostart.Uninstall(); err != nil {
		fmt.Fprintf(out, "  note: %v\n", err)
	}
	if data, err := paths.DataDir(); err == nil {
		fmt.Fprintln(out, "Removing the extension copy...")
		_ = os.RemoveAll(filepath.Join(data, "extension"))
	}
	if target, err := installedExe(); err == nil {
		fmt.Fprintf(out, "Removing %s ...\n", target)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(out, "  could not remove it (%v). You can delete it by hand.\n", err)
		}
		if runtime.GOOS == "windows" {
			_ = os.Remove(filepath.Join(filepath.Dir(target), BackgroundExeName))
			removeFromPath(filepath.Dir(target))
		}
	}
	if purge {
		fmt.Fprintln(out, "Deleting settings and logs...")
		if d, err := paths.ConfigDir(); err == nil {
			_ = os.RemoveAll(d)
		}
		if d, err := paths.DataDir(); err == nil {
			_ = os.RemoveAll(d)
		}
	}
	fmt.Fprintln(out, "\nDone. Remove the extension from your browser's extensions page too (it can't be removed from outside the browser).")
	return nil
}

func sameFile(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ai, bi)
}

// copyFile replaces dst atomically. The retry loop is for Windows, where the
// old program may take a moment to release its file after being stopped.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	var last error
	for i := 0; i < 20; i++ {
		if last = os.Rename(tmp, dst); last == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	os.Remove(tmp)
	return last
}

// allowToRun clears macOS's "downloaded from the internet" flag, which would
// otherwise make Gatekeeper refuse to start the program.
func allowToRun(exe string) {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("xattr", "-d", "com.apple.quarantine", exe).Run()
	}
}

// ensureOnPath makes `ytm-beacon` usable from a terminal. On Windows it adds
// the folder to the user PATH; elsewhere it only tells the user if it's missing.
func ensureOnPath(dir string) string {
	if runtime.GOOS == "windows" {
		if err := addToPath(dir); err != nil {
			return fmt.Sprintf("Could not add %s to your PATH (%v). Use the full path to run it from a terminal.", dir, err)
		}
		return "Open a new terminal window to use the `ytm-beacon` command."
	}
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return ""
		}
	}
	return fmt.Sprintf("Tip: %s is not on your PATH. Add this to your shell profile to use `ytm-beacon` anywhere:\n  export PATH=\"%s:$PATH\"", dir, strings.ReplaceAll(dir, os.Getenv("HOME"), "$HOME"))
}
