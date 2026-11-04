# Troubleshooting

**Start here:** run `ytm-beacon doctor`. It prints a checklist with a fix (`->`) for each problem.
If you open an issue, paste its whole output.

> Can't run `ytm-beacon` in a terminal? On Windows open a **new** PowerShell window after
> installing (the installer adds it to your PATH), or double-click `ytm-beacon.exe` in
> `%LOCALAPPDATA%\ytm-beacon`. On Linux/macOS it lives in `~/.local/bin`.

You can also open the dashboard at <http://127.0.0.1:41713/>: it shows the same checks, a **Show
log** button, a **Copy diagnostics** button, and a **Show a test song on Discord** button that tests
the Discord side without needing YouTube Music or the extension.

## Which part is broken?

Use the test song button on the dashboard.

| Test song shows on Discord? | Song from YouTube Music shows? | The problem is in... |
| --- | --- | --- |
| No | No | The app <-> Discord link. See [Discord isn't reachable](#discord-isnt-reachable) or [Discord is connected but nothing shows](#discord-is-connected-but-nothing-shows). |
| Yes | No | The extension <-> app link. See [The extension isn't reporting](#the-extension-isnt-reporting). |
| Yes | Yes | Nothing! |

## Discord isn't reachable

`doctor` says *"Cannot reach Discord"* or *"no Discord IPC socket found"*.

- Open the Discord **desktop app**. The website and the mobile app can't show Rich Presence.
- Wait a few seconds: the app retries by itself, with no restart needed.
- Discord and YTM Beacon must run as the **same user**. On Windows, if one of them is started with
  *Run as administrator*, start both normally.
- **Invalid Client ID / "Discord refused the connection"**: the Discord application ID is wrong. Clear
  the *Discord application ID* field on the dashboard to use the built-in one, or copy the
  *Application ID* (a 17-19 digit number) from the Discord Developer Portal.

### Linux: Flatpak and Snap Discord

YTM Beacon looks for Discord's socket (`discord-ipc-0`) in these places, so the usual setups work
without any changes:

- `$XDG_RUNTIME_DIR` (native Discord)
- `$XDG_RUNTIME_DIR/app/com.discordapp.Discord` (Flatpak Discord)
- `$XDG_RUNTIME_DIR/snap.discord` (Snap Discord)
- `$XDG_RUNTIME_DIR/app/*` and `$XDG_RUNTIME_DIR/.flatpak/*/xdg-run` (Vesktop, Legcord and other
  Flatpak clients)
- `$TMPDIR`, `/tmp`

`ytm-beacon doctor` prints the folders it searched. To see which Discord sockets exist:

```bash
ls "$XDG_RUNTIME_DIR"/discord-ipc-* "$XDG_RUNTIME_DIR"/app/*/discord-ipc-* 2>/dev/null
```

If Flatpak Discord created no socket, give it access to the runtime folder it needs and restart it:

```bash
flatpak override --user --filesystem=xdg-run/app/com.discordapp.Discord:create com.discordapp.Discord
```

If you run the *browser* as a Flatpak/Snap, that's fine: the extension only needs to reach
`127.0.0.1`, which sandboxes allow.

## Discord is connected but nothing shows

- **User Settings > Activity Privacy**: sharing your activity must be on.
- Your status must not be **Invisible**.
- Look at your profile (click your name), not the member list. Cards appear on profiles first.
- You can't see your own *Listen on YouTube Music* button; other people can.
- If you use **Vesktop/Vencord** or another client, make sure it has its Rich Presence bridge
  (arRPC) enabled.
- Another program may also be setting your activity (a game, Spotify). Discord shows several
  activities, but some clients only show the most recent one.

## The extension isn't reporting

`doctor` says *"The browser extension has never reported in"* (or *went quiet*).

1. Is the extension installed and **enabled** on your browser's extensions page? (Chrome-family
   browsers can disable "unpacked" extensions after a browser update: check for a *Re-enable* prompt.)
2. **Click the extension's toolbar icon.** It checks your YouTube Music tab and says what is wrong:
   no tab open, the extension isn't running in the tab (there is a **Reload the tab** button), an ad
   is playing, or nothing is loaded yet. The extension normally starts itself in tabs that were already
   open, but a tab that your browser has put to sleep may need a reload.
3. A red **!** on the toolbar icon means the extension can't reach the app. Run `doctor`; the app is
   probably not running.
4. Ad blockers and enhancer extensions can interfere in two ways:
   - Some replace `navigator.mediaSession` inside the page, so YouTube Music's song info never reaches
     the browser's real media session. YTM Beacon works around this by also reading the song from inside
     the page, and as a last resort from the on-screen player. If the popup still says *"Nothing is
     playing yet"* while a song plays, it lists what each source saw (`page script`, `in-page song`,
     `browser song`). Please include that line in your bug report.
   - Blocking requests to `127.0.0.1` stops the extension from reaching the app. Try with them
     disabled for `music.youtube.com`.
5. If your browser asks to *"connect to devices on your local network"*, choose **Allow**. That is the
   extension talking to the app on your own computer.

### Firefox

- Temporary add-ons (loaded from `about:debugging`) disappear when Firefox restarts. Use the signed
  `.xpi` from the Releases page for a permanent install.
- Firefox may ask you to grant the extension access to `127.0.0.1` (the app on your computer):
  *about:addons > YTM Beacon > Permissions*.

## The card looks wrong

- **Progress bar is off after seeking:** it re-syncs within a second or two of a seek.
- **Two seconds of delay when skipping songs:** Discord only allows a few updates per second, so
  rapid changes are spaced out on purpose.
- **No album art, or the YTM logo instead:** the track has no artwork, or *Show album art* is off.
- **Plays but shows "Paused":** another YouTube Music tab is paused. The app follows the tab that is
  playing; close or reload the stale one.
- **Ads:** nothing is shown while an ad plays (the ad's details would otherwise appear as your song).

## Installation problems

- **Windows SmartScreen / antivirus:** the app is not code-signed (certificates cost money), so
  Windows may warn. Click *More info > Run anyway*. The one-line PowerShell installer avoids the
  warning. You can also build it yourself: see [DEVELOPMENT.md](DEVELOPMENT.md).
- **macOS "cannot be opened because the developer cannot be verified":** install with the `curl`
  one-liner instead of downloading in the browser. If you already downloaded it, run
  `xattr -d com.apple.quarantine ./ytm-beacon` once.
- **`ytm-beacon: command not found` (Linux/macOS):** add `~/.local/bin` to your PATH. The installer
  prints the exact line.
- **A port is busy:** the app uses the first free port from 41713 to 41717 and the extension tries
  them all. If all five are taken by other programs, `ytm-beacon run` says so.
- **Start at login doesn't work on Linux:** the app uses a *systemd user service* if available
  (check with `systemctl --user status ytm-beacon`), otherwise an autostart entry in
  `~/.config/autostart`.
- **Logs:** `ytm-beacon doctor` prints the last lines; the full file is shown on the dashboard
  and lives next to the app's data (`~/.local/share/ytm-beacon/ytm-beacon.log` on Linux,
  `~/Library/Application Support/ytm-beacon/` on macOS, `%LOCALAPPDATA%\ytm-beacon\` on Windows).
  Run `ytm-beacon run --debug` in a terminal to log every update.

## Updating and uninstalling

- **Update:** `ytm-beacon update` (add `--check` to only look). The extension reloads itself within a
  few seconds. If your extension is from before that feature existed, reload it once on the browser's
  extensions page; after that updates are automatic.
- **`update` says "no published release found":** the project has no public release yet, or you are
  offline.
- **Uninstall:** `ytm-beacon uninstall` (add `--purge` to also delete settings), then remove the
  extension on the browser's extensions page. One-line uninstallers and manual steps for every
  system are in the [README](../README.md#uninstalling).

## Still stuck?

[Open an issue](https://github.com/dash1101/ytm-beacon/issues/new/choose) and paste the output of
`ytm-beacon doctor`. Say which browser, which Discord app (native, Flatpak, Vesktop...) and which
operating system you use.
