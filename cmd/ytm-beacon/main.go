// Command ytm-beacon shows what you're playing on YouTube Music as your
// Discord Rich Presence. See `ytm-beacon help`.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	beacon "github.com/dash1101/ytm-beacon"
	"github.com/dash1101/ytm-beacon/internal/autostart"
	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/discord"
	"github.com/dash1101/ytm-beacon/internal/doctor"
	"github.com/dash1101/ytm-beacon/internal/extension"
	"github.com/dash1101/ytm-beacon/internal/installer"
	"github.com/dash1101/ytm-beacon/internal/paths"
	"github.com/dash1101/ytm-beacon/internal/presence"
	"github.com/dash1101/ytm-beacon/internal/server"
	"github.com/dash1101/ytm-beacon/internal/updater"
	"github.com/dash1101/ytm-beacon/internal/version"
)

const usage = `YTM Beacon - show what you're playing on YouTube Music in your Discord profile

Usage: ytm-beacon <command>

Commands:
  install      Set everything up: copy the program, unpack the browser
               extension, start at login, and start now
  update       Download and install the latest version
               (add --check to only see whether there is one)
  hide [time]  Stop showing on Discord, until you run "show" or for a while
               (examples: hide, hide 1h, hide 30m)
  show         Show on Discord again
  doctor       Check that everything works and explain what to fix
  uninstall    Remove everything: stops the app, removes start-at-login and
               the installed files (add --purge to also delete your settings)
  extension    Show where the browser extension is (and how to add it)
  run          Run in the foreground (what start-at-login does)
  stop         Stop the running app
  version      Print the version

Dashboard and settings: http://127.0.0.1:%d/ (while running)
Help and bug reports:   %s
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		// The Windows background build is started with no arguments by some
		// launchers; it should simply run.
		if strings.Contains(strings.ToLower(filepath.Base(os.Args[0])), "-bg") {
			args = []string{"run"}
		} else {
			os.Exit(finish(interactive()))
		}
	}

	var err error
	switch args[0] {
	case "run":
		err = cmdRun(args[1:])
	case "install":
		ifl := flag.NewFlagSet("install", flag.ExitOnError)
		noOpen := ifl.Bool("no-open", false, "don't open the setup page in the browser")
		_ = ifl.Parse(args[1:])
		err = installer.Install(os.Stdout, extFS(), installer.Options{NoOpen: *noOpen})
	case "update":
		ufl := flag.NewFlagSet("update", flag.ExitOnError)
		check := ufl.Bool("check", false, "only report whether a newer version exists")
		force := ufl.Bool("force", false, "reinstall even if already up to date")
		_ = ufl.Parse(args[1:])
		err = updater.Run(os.Stdout, updater.Options{Check: *check, Force: *force})
	case "uninstall":
		ufl := flag.NewFlagSet("uninstall", flag.ExitOnError)
		purge := ufl.Bool("purge", false, "also delete settings and logs")
		_ = ufl.Parse(args[1:])
		err = installer.Uninstall(os.Stdout, *purge)
	case "stop":
		if _, e := server.FetchStatus(); e != nil {
			fmt.Println("YTM Beacon is not running.")
		} else if err = server.Quit(); err == nil {
			fmt.Println("Stopped.")
		}
	case "hide":
		err = cmdHide(args[1:])
	case "show":
		err = cmdShow()
	case "doctor":
		os.Exit(finish(doctor.Run(os.Stdout)))
	case "extension":
		err = cmdExtension(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("ytm-beacon %s (%s) %s/%s\n", version.Version, version.Commit, runtime.GOOS, runtime.GOARCH)
	case "help", "--help", "-h":
		fmt.Printf(usage, server.Ports[0], version.RepoURL)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q.\n\n", args[0])
		fmt.Fprintf(os.Stderr, usage, server.Ports[0], version.RepoURL)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(finish(1))
	}
	os.Exit(finish(0))
}

// finish keeps the window open when the program was started by double-clicking
// it on Windows (otherwise the output would vanish instantly).
func finish(code int) int {
	if ownsConsole() {
		fmt.Print("\nPress Enter to close this window...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	return code
}

func extFS() fs.FS {
	sub, err := fs.Sub(beacon.Extension, "extension")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return sub
}

// interactive handles running the program with no arguments, e.g. by
// double-clicking it: offer to install, or show the current state.
func interactive() int {
	if _, err := server.FetchStatus(); err == nil {
		fmt.Println("YTM Beacon is already running. Checking that everything is fine:")
		fmt.Println()
		return doctor.Run(os.Stdout)
	}
	if on, _ := autostart.Installed(); !on && isTerminal(os.Stdin) {
		fmt.Println("YTM Beacon shows what you're playing on YouTube Music in your Discord profile.")
		fmt.Println("It isn't installed yet.")
		// On Windows the window only exists because the program was
		// double-clicked, so the intent is clear: just install.
		if !ownsConsole() {
			fmt.Print("\nInstall it now? [Y/n] ")
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && strings.TrimSpace(line) == "" {
				// No answer at all (input isn't really a keyboard): don't install unasked.
				fmt.Println("\n\nNothing was installed. Run  ytm-beacon install  when you're ready.")
				return 0
			}
			if a := strings.ToLower(strings.TrimSpace(line)); a != "" && a != "y" && a != "yes" {
				fmt.Printf(usage, server.Ports[0], version.RepoURL)
				return 0
			}
		}
		fmt.Println()
		if err := installer.Install(os.Stdout, extFS(), installer.Options{}); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
		return 0
	}
	fmt.Printf(usage, server.Ports[0], version.RepoURL)
	return 0
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func cmdExtension(args []string) error {
	fl := flag.NewFlagSet("extension", flag.ExitOnError)
	out := fl.String("out", "", "write the extension into this folder instead of the default place")
	zip := fl.Bool("zip", false, "with --out: write ytm-beacon-<browser>.zip files instead of folders")
	_ = fl.Parse(args)

	if *out == "" {
		for _, b := range extension.Browsers {
			dir, err := paths.ExtensionDir(b)
			if err != nil {
				return err
			}
			if err := extension.Write(extFS(), b, version.Version, dir); err != nil {
				return err
			}
			fmt.Printf("%-8s %s\n", b+":", dir)
		}
		fmt.Println("\nChrome, Brave, Edge...: open chrome://extensions, turn on Developer mode, click \"Load unpacked\", choose the chrome folder.")
		fmt.Println("Firefox: install the signed .xpi from the Releases page, or load the firefox folder's manifest.json from about:debugging.")
		return nil
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	for _, b := range extension.Browsers {
		if *zip {
			p := filepath.Join(*out, "ytm-beacon-"+b+".zip")
			f, err := os.Create(p)
			if err != nil {
				return err
			}
			err = extension.Zip(extFS(), b, version.Version, f)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
			fmt.Println("wrote", p)
		} else {
			dir := filepath.Join(*out, b)
			if err := extension.Write(extFS(), b, version.Version, dir); err != nil {
				return err
			}
			fmt.Println("wrote", dir)
		}
	}
	return nil
}

// teeWriter writes log lines to the log file and, if possible, to the terminal.
// A missing terminal (Windows background build) must not stop the logging.
type teeWriter struct{ file, term io.Writer }

func (t teeWriter) Write(p []byte) (int, error) {
	n, err := t.file.Write(p)
	_, _ = t.term.Write(p)
	return n, err
}

func openLog(path string) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		_ = os.Rename(path, path+".old") // keep one previous log
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func cmdRun(args []string) error {
	fl := flag.NewFlagSet("run", flag.ExitOnError)
	debug := fl.Bool("debug", os.Getenv("YTM_BEACON_DEBUG") != "", "log every presence update")
	_ = fl.Parse(args)

	logPath, _ := paths.LogFile()
	var logw io.Writer = os.Stderr
	if lf, err := openLog(logPath); err == nil {
		defer lf.Close()
		logw = teeWriter{file: lf, term: os.Stderr}
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logw, &slog.HandlerOptions{Level: level})))

	cfgPath, _ := paths.ConfigFile()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Warn("settings problem", "error", err)
	}

	listener, port, err := server.Listen()
	if errors.Is(err, server.ErrAlreadyRunning) {
		fmt.Printf("YTM Beacon is already running (port %d).\n", port)
		return nil
	}
	if err != nil {
		return err
	}

	tracker := presence.NewTracker()
	engine := presence.NewEngine(cfg, tracker, func(ctx context.Context, id string) (discord.Conn, error) {
		c, err := discord.Connect(ctx, discord.Options{ClientID: id})
		if err != nil {
			return nil, err // do not wrap a nil *Client in the interface
		}
		return c, nil
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	marker, _ := paths.ConnectedMarker()
	srv := server.New(server.Deps{
		Config: cfg, Tracker: tracker, Engine: engine, LogPath: logPath, Quit: stop,
		ExtensionBuild: extension.Build(extFS(), version.Version), ConnectedMarker: marker,
	}, port)
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}

	slog.Info("YTM Beacon started", "version", version.Version, "port", port, "os", runtime.GOOS, "arch", runtime.GOARCH)
	if cfg.Get().EffectiveClientID() == "" {
		slog.Warn("no Discord application ID configured; open the dashboard to add one", "url", fmt.Sprintf("http://127.0.0.1:%d/", port))
	}

	engineDone := make(chan struct{})
	go func() {
		engine.Run(ctx)
		close(engineDone)
	}()
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(listener) }()

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			stop()
			<-engineDone
			return fmt.Errorf("web server stopped: %w", err)
		}
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	<-engineDone // the engine clears the Discord presence before it returns
	return nil
}
