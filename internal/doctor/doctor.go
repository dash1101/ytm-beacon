// Package doctor implements `ytm-beacon doctor`: a self-check that explains in
// plain words what is wrong and how to fix it. Its output is designed to be
// pasted into a bug report.
package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/dash1101/ytm-beacon/internal/autostart"
	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
	"github.com/dash1101/ytm-beacon/internal/paths"
	"github.com/dash1101/ytm-beacon/internal/presence"
	"github.com/dash1101/ytm-beacon/internal/server"
	"github.com/dash1101/ytm-beacon/internal/version"
)

type report struct {
	w            io.Writer
	fails, warns int
}

func (r *report) line(tag, format string, a ...any) {
	fmt.Fprintf(r.w, "  %-5s %s\n", tag, fmt.Sprintf(format, a...))
}
func (r *report) ok(f string, a ...any)   { r.line("OK", f, a...) }
func (r *report) info(f string, a ...any) { r.line("", f, a...) }
func (r *report) warn(f string, a ...any) { r.warns++; r.line("WARN", f, a...) }
func (r *report) fail(f string, a ...any) { r.fails++; r.line("FAIL", f, a...) }
func (r *report) fix(f string, a ...any)  { fmt.Fprintf(r.w, "        -> %s\n", fmt.Sprintf(f, a...)) }

// Run prints the report and returns the process exit code (0 = no problems).
func Run(w io.Writer) int {
	r := &report{w: w}
	exe, _ := os.Executable()
	fmt.Fprintf(w, "YTM Beacon doctor\n")
	fmt.Fprintf(w, "  version %s (%s), %s/%s, %s\n\n", version.Version, version.Commit, runtime.GOOS, runtime.GOARCH, exe)

	// Settings.
	cfgPath, _ := paths.ConfigFile()
	store, err := config.Load(cfgPath)
	if err != nil {
		r.fail("Settings file problem: %v", err)
		r.fix("Fix or delete %s, then run this again.", cfgPath)
	}
	cfg := store.Get()
	clientID := cfg.EffectiveClientID()

	// Start at login.
	if on, how := autostart.Installed(); on {
		r.ok("Starts automatically at login (%s)", how)
	} else {
		r.warn("Not set to start at login")
		r.fix("Run:  ytm-beacon install")
	}

	// Is it running?
	st, runErr := server.FetchStatus()
	if runErr != nil {
		r.fail("The app is not running")
		r.fix("Run:  ytm-beacon install   (sets it up and starts it)")
		r.fix("or start it by hand to see errors:  ytm-beacon run")
		checkDiscordDirectly(r, clientID)
	} else {
		r.ok("The app is running (version %s, port %d, up %s)", st.Version, st.Port, (time.Duration(st.UptimeSec) * time.Second).String())
		if st.Version != version.Version {
			r.warn("The running app is version %s but this program is %s", st.Version, version.Version)
			r.fix("Run:  ytm-beacon install   (updates the running copy)")
		}
		reportRunning(r, st)
	}

	// Recent log lines are the most useful thing in a bug report.
	if logPath, err := paths.LogFile(); err == nil {
		if tail := server.TailFile(logPath, 15); strings.TrimSpace(tail) != "" {
			fmt.Fprintf(w, "\nLast log lines (%s):\n%s", logPath, indent(tail))
		}
	}

	fmt.Fprintln(w)
	switch {
	case r.fails > 0:
		fmt.Fprintf(w, "%d problem(s) found. Follow the -> steps above. Still stuck? Open an issue at %s/issues and paste this whole output.\n", r.fails, version.RepoURL)
		return 1
	case r.warns > 0:
		fmt.Fprintf(w, "No blocking problems, %d warning(s).\n", r.warns)
	default:
		fmt.Fprintln(w, "Everything looks good.")
	}
	return 0
}

func reportRunning(r *report, st *server.Status) {
	d := st.Discord
	switch d.State {
	case presence.StateConnected:
		r.ok("Connected to Discord (signed in as %s)", orDash(d.User))
		if d.LastError != "" {
			r.warn("Discord rejected the last update: %s", d.LastError)
			r.fix("This is usually temporary. If it persists, include this report in an issue.")
		}
	case presence.StateNoClientID:
		r.fail("No Discord application ID is configured")
		r.fix("Open http://127.0.0.1:%d/ and add one under Settings (see the README, \"Using your own Discord app\").", st.Port)
	default:
		r.fail("Cannot reach Discord: %s", orDash(d.LastError))
		discordHints(r, d.LastError)
	}

	seen := st.Extension.LastSeenAgoSec
	switch {
	case seen == nil:
		r.fail("The browser extension has never reported in")
		r.fix("Add the extension to your browser: the setup page at http://127.0.0.1:%d/ shows the steps", st.Port)
		r.fix("(or run `ytm-beacon extension` to see the folder). Then play a song on https://music.youtube.com.")
		r.fix("Already installed? Click the extension's toolbar icon: it says what is wrong in your YouTube Music tab.")
	case *seen > 20:
		r.warn("The browser extension last reported %ds ago", *seen)
		r.fix("Open or reload your YouTube Music tab. Make sure the extension is enabled.")
	default:
		r.ok("The browser extension is reporting (%d source(s))", len(st.Extension.Sources))
	}

	if np := st.NowPlaying; np != nil {
		r.info("Now playing: %s - %s [%s]", np.Title, np.Artist, np.State)
	}
	if st.PresenceActive {
		r.ok("Presence is visible on Discord")
	} else if st.Discord.State == presence.StateConnected && st.NowPlaying != nil {
		r.warn("Something is playing but nothing is showing on Discord")
		r.fix("Check Discord > Settings > Activity Privacy > \"Share my activity\" is on, and that you are not set to Invisible.")
	}
}

func checkDiscordDirectly(r *report, clientID string) {
	eps := discord.Endpoints()
	if len(eps) == 0 {
		r.fail("Discord is not running (no Discord IPC socket found)")
		r.fix("Start the Discord desktop app (the website version cannot show Rich Presence).")
		if runtime.GOOS != "windows" {
			r.info("Looked in: %s", strings.Join(discord.SearchDirs(), ", "))
		}
		discordHints(r, "")
		return
	}
	if clientID == "" {
		r.warn("Discord socket found, but no application ID is configured to test with")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, err := discord.Connect(ctx, discord.Options{ClientID: clientID})
	if err != nil {
		r.fail("Could not connect to Discord: %v", err)
		discordHints(r, err.Error())
		return
	}
	defer c.Close()
	r.ok("Discord is reachable (signed in as %s, via %s)", orDash(c.Info().User), c.Info().Endpoint)
}

func discordHints(r *report, errText string) {
	low := strings.ToLower(errText)
	switch {
	case strings.Contains(low, "invalid client"):
		r.fix("The Discord application ID is wrong. Copy the \"Application ID\" from discord.com/developers/applications.")
	case strings.Contains(low, "permission denied") || strings.Contains(low, "access is denied"):
		r.fix("Discord and YTM Beacon must run as the same user. If one of them is \"Run as administrator\", start both normally.")
	default:
		r.fix("Make sure the Discord desktop app is open and you are logged in. YTM Beacon reconnects by itself within seconds.")
		if runtime.GOOS == "linux" {
			r.fix("Flatpak/Snap Discord: the app needs access to Discord's socket. See README > Troubleshooting > Linux.")
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n") + "\n"
}
