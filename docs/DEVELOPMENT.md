# Development

## Layout

```
cmd/ytm-beacon/        CLI entry point (run, install, doctor, extension, ...)
internal/
  discord/             Discord IPC client (unix socket / Windows named pipe)
  presence/            Tracker (which tab wins), BuildActivity (card contents), Engine (connect/retry/send)
  server/              The 127.0.0.1 HTTP API, the security gate, and the dashboard page
  config/              config.json
  autostart/           systemd / launchd / Windows Run key
  installer/           `install` and `uninstall` (first-run setup page, Windows-safe file replacement)
  updater/             `update`: latest release -> checksum -> run its installer
  extension/           unpacks the embedded browser extension (folder or zip)
  doctor/              `ytm-beacon doctor`
  paths/               where files live on each OS
extension/             The browser extension (Manifest V3; one source, a manifest per browser)
tools/mockdiscord/     A fake Discord, so you can develop without touching the real one
tools/gen_icons.py     Renders the icons and Discord play/pause badges from assets/*.svg
scripts/               release build, install.sh, install.ps1
embed.go               Embeds extension/ into the binary
```

Requirements: Go (see `go.mod`), nothing else. Python + Pillow and an SVG renderer (rsvg-convert,
Inkscape or cairosvg) only for regenerating icons.

## Build, test, run

```bash
go vet ./...
go test -race ./...
go build -o ytm-beacon ./cmd/ytm-beacon
```

Cross-compile (the code is pure Go, so `CGO_ENABLED=0` works everywhere):

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/ytm-beacon
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/ytm-beacon
```

### Run against a fake Discord (Linux/macOS)

```bash
mkdir -p /tmp/mock && go run ./tools/mockdiscord -dir /tmp/mock
# in another terminal (macOS: use TMPDIR instead of XDG_RUNTIME_DIR):
XDG_RUNTIME_DIR=/tmp/mock YTM_BEACON_HOME=/tmp/ytmb-home YTM_BEACON_CLIENT_ID=123456789012345678 \
  go run ./cmd/ytm-beacon run --debug
# fake the extension:
curl -X POST localhost:41713/v1/update -d \
  '{"source":"dev","state":"playing","title":"Song","artist":"Artist","position":10,"duration":200}'
```

`YTM_BEACON_HOME` moves config, data and the install location into one folder, so experiments never
touch your real install. `mockdiscord -reject-id` and `-reject-new-fields` simulate a bad application
ID and an older Discord client.

Keep the socket path short: Unix sockets are limited to about 100 characters.

### The extension

Build the folders and load one in your browser:

```bash
go run ./cmd/ytm-beacon extension --out /tmp/ytmb-ext   # creates chrome/ and firefox/
```

`extension/manifest.chrome.json` and `manifest.firefox.json` share the same scripts; the build picks
the right one and writes it as `manifest.json`. `__VERSION__` is replaced by the app version. A test
checks that every file a manifest or the popup refers to is really packaged.

If you change files in `extension/`, rebuild the app (they're embedded) and re-run
`ytm-beacon extension`, then reload the extension in your browser.

## Artwork

The SVGs in `assets/` are the sources: `icon.svg` (the logo), `icon-small.svg` (the same logo simplified for
16 and 32 px), and `badge-play.svg` / `badge-pause.svg` (the small round badge on the Discord card). After
editing one, run `python3 tools/gen_icons.py`: it writes the extension icons (and the grey "switched off"
ones), `assets/logo.png`, `play.png`, `pause.png`, and the dashboard's copy `internal/server/icon.svg`
(a test fails if that copy is out of date).

Discord fetches the logo and badges from this repository itself. A release build points at its own tag
(`.../ytm-beacon/v0.1.5/assets/logo.png`), so a release's pictures never change under it and new art
gets a new address; other builds use `main`. Don't rename or delete the PNGs in `assets/`: older
releases point at them.

## How the pieces talk

1. `content.js` (on music.youtube.com) reads `navigator.mediaSession.metadata` and the `<video>`
   element once a second. It sends a message when the track or play state changes, when the user
   seeks, and every 5 seconds as a heartbeat. YouTube Music's DOM is only used as a fallback.
   `page.js` runs in the page's own world and copies `navigator.mediaSession.metadata` to an attribute
   on `<html>`, because some extensions replace `mediaSession` inside the page and the extension's
   isolated world then sees nothing. `content.js` prefers that copy, then the real object, then the
   on-screen player (`domMetadata`).
2. `background.js` forwards the message to `http://127.0.0.1:<port>/v1/update` (ports 41713-41717).
   It keeps no state because browsers can stop it at any time. The app's reply carries the extension
   **build id** (a hash of the extension sources, `extension.Build`); when it differs from the build
   baked into `background.js` (`__BUILD__`), the extension calls `runtime.reload()` once (guarded by
   `storage.local.reloadedFor`), so updating the app updates an unpacked extension without clicks.
3. The app's `Tracker` keeps the latest report per tab and picks the one to show (playing beats
   paused, newest wins). Reports expire after 45 s, so closing the browser clears the card.
4. The `Engine` runs once a second and on every update. It builds the activity, compares it with what
   it last sent, and only talks to Discord when something visible changed (never faster than every
   2 s). It reconnects with a backoff while Discord is closed.
5. If Discord rejects the activity, the engine retries once with only the basic fields
   (`BuildActivity(..., basic=true)`) and keeps using that, so an older Discord client still works.

## Releasing

```bash
git tag v0.1.0 && git push origin v0.1.0
```

Users update with `ytm-beacon update` (see `internal/updater`) and remove it with `ytm-beacon uninstall`
or `scripts/uninstall.sh|ps1`. `.github/workflows/release.yml` runs the tests, runs `scripts/build-release.sh`, and attaches to the
GitHub release: one archive per OS/arch, the two extension zips, `checksums.txt`, and (if the AMO
secrets exist) the signed Firefox `.xpi`. The installers in `scripts/` download from
`releases/latest/download`, so asset names must not contain the version.

Build locally with `scripts/build-release.sh dev` (needs `zip`).

## Style

Plain Go, standard library first (the only dependency is `go-winio`, used on Windows only).
Error messages are written for end users: say what happened and what to do about it. Anything a user
might need to fix should show up in `doctor`.
