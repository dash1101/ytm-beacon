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
	"github.com/dash1101/ytm-beacon/internal/presence"
	"github.com/dash1101/ytm-beacon/internal/server"
	"github.com/dash1101/ytm-beacon/internal/version"
)

// BackgroundExeName is the Windows build without a console window. When it sits
// next to the main program it is what starts at login, so no black window ever
// appears. Other systems don't need it.
const BackgroundExeName = "ytm-beacon-bg.exe"

// Options tune `install`.
type Options struct {
	// NoOpen stops the installer from opening the setup page in the browser.
	// Setting YTM_BEACON_NO_OPEN=1 does the same.
	NoOpen bool
}

func installedExe() (string, error) {
	dir, err := paths.BinDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, paths.ExeName("ytm-beacon")), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Install does everything. extSrc is the embedded extension (rooted at its manifest files).
func Install(out io.Writer, extSrc fs.FS, opt Options) error {
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

	// Has an extension ever reported in? If so this is an update, not a first setup.
	firstRun := true
	if m, err := paths.ConnectedMarker(); err == nil && fileExists(m) {
		firstRun = false
	}

	fmt.Fprintln(out, "Installing YTM Beacon...")
	if err := server.Quit(); err != nil {
		fmt.Fprintf(out, "  --  could not stop the running copy (%v); continuing\n", err)
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
	fmt.Fprintf(out, "  OK  Program installed: %s\n", target)

	for _, b := range extension.Browsers {
		dir, err := paths.ExtensionDir(b)
		if err != nil {
			return err
		}
		if err := extension.Write(extSrc, b, version.Version, dir); err != nil {
			return fmt.Errorf("could not unpack the %s extension: %w", b, err)
		}
	}
	fmt.Fprintln(out, "  OK  Browser extension unpacked")

	how, err := autostart.Install(daemon)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "  OK  Starts when you log in: %s\n", how)

	var st *server.Status
	for i := 0; i < 40; i++ {
		if s, err := server.FetchStatus(); err == nil {
			st = s
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	// The app answers before its first try at reaching Discord has finished.
	// Give it a moment, so we never claim Discord is closed when it isn't.
	for i := 0; st != nil && st.Discord.State == presence.StateConnecting && i < 16; i++ {
		time.Sleep(250 * time.Millisecond)
		if s, err := server.FetchStatus(); err == nil {
			st = s
		}
	}
	if st == nil {
		fmt.Fprintln(out, "  !!  It did not start. Run `ytm-beacon doctor` to see why.")
	} else {
		fmt.Fprintln(out, "  OK  Running")
		switch st.Discord.State {
		case presence.StateConnected:
			fmt.Fprintf(out, "  OK  Connected to Discord as %s\n", st.Discord.User)
		case presence.StateNoClientID:
			fmt.Fprintf(out, "  !!  No Discord application ID is set. Open http://127.0.0.1:%d/ to add one.\n", st.Port)
		case presence.StateConnecting:
			fmt.Fprintln(out, "  --  Still connecting to Discord. Run `ytm-beacon doctor` in a moment if it doesn't show up.")
		default:
			fmt.Fprintln(out, "  --  Discord isn't open yet. It connects by itself when you open the Discord app.")
		}
	}

	dash := fmt.Sprintf("http://127.0.0.1:%d/", server.Ports[0])
	if st != nil {
		dash = fmt.Sprintf("http://127.0.0.1:%d/", st.Port)
	}
	if firstRun {
		chrome, _ := paths.ExtensionDir("chrome")
		firefox, _ := paths.ExtensionDir("firefox")
		fmt.Fprintf(out, "\nOne step left: add the browser extension (one time).\n")
		opened := false
		if !opt.NoOpen && os.Getenv("YTM_BEACON_NO_OPEN") == "" && st != nil {
			opened = openURL(dash) == nil
		}
		if opened {
			fmt.Fprintf(out, "  The setup page just opened in your browser; it shows the exact steps.\n")
		} else {
			fmt.Fprintf(out, "  Open %s for the exact steps.\n", dash)
		}
		fmt.Fprintf(out, "\n  Chrome, Brave, Edge, Opera, Vivaldi: open chrome://extensions, turn on Developer mode,\n")
		fmt.Fprintf(out, "  click \"Load unpacked\" and choose:\n    %s\n", chrome)
		fmt.Fprintf(out, "  Firefox: open about:debugging#/runtime/this-firefox, click \"Load Temporary Add-on...\"\n")
		fmt.Fprintf(out, "  and choose manifest.json in:\n    %s\n", firefox)
		fmt.Fprintf(out, "\nThen play a song on music.youtube.com. Anything wrong? Run:  ytm-beacon doctor\n")
	} else {
		fmt.Fprintf(out, "\nDone. The browser extension reloads itself within a few seconds, so there is nothing to do in your browser.\n")
		fmt.Fprintf(out, "Dashboard and settings: %s\n", dash)
	}
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
		if m, err := paths.ConnectedMarker(); err == nil {
			_ = os.Remove(m)
		}
	}
	if target, err := installedExe(); err == nil {
		fmt.Fprintf(out, "Removing %s ...\n", target)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			// Windows: this very program is running from that file.
			scheduleDelete(target)
			fmt.Fprintln(out, "  it will be removed a moment after this window closes")
		}
		if runtime.GOOS == "windows" {
			_ = os.Remove(filepath.Join(filepath.Dir(target), BackgroundExeName))
			_ = os.Remove(target + ".old")
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
	fmt.Fprintln(out, "\nYTM Beacon is removed. One thing is left that only you can do: remove the extension from your browser")
	fmt.Fprintln(out, "(open its extensions page, find YTM Beacon, click Remove).")
	if !purge {
		fmt.Fprintln(out, "Your settings and log were kept, in case you install it again. Run `ytm-beacon uninstall --purge` to delete them too.")
	} else {
		fmt.Fprintln(out, "Nothing else of it remains.")
	}
	return nil
}

func sameFile(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ai, bi)
}

// copyFile replaces dst atomically. The retry loop is for Windows, where the
// old program may take a moment to release its file after being stopped, and
// where a running program (for example the one doing an update) can't be
// overwritten but can be renamed out of the way.
func copyFile(src, dst string) error {
	_ = os.Remove(dst + ".old") // left over from a previous replace of a running program
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
		if runtime.GOOS == "windows" {
			old := dst + ".old"
			if os.Rename(dst, old) == nil {
				if last = os.Rename(tmp, dst); last == nil {
					return nil
				}
			}
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

// openURL opens a page in the user's default browser without waiting for it.
func openURL(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap it; xdg-open exits quickly
	return nil
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
