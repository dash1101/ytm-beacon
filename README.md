# YTM Beacon

Show what you're listening to on **YouTube Music** as your **Discord** status: song, artist,
album art, a live progress bar, and a "Listen on YouTube Music" button.

- **Works on Windows, macOS and Linux**, with Chrome, Brave, Edge, Opera, Vivaldi and Firefox.
- **One small download, nothing else to install.** No Node, no Python, no Discord login.
- **Starts by itself** when you log in and **reconnects by itself** when you restart Discord.
- **Easy to debug:** `ytm-beacon doctor` checks everything and tells you what to fix.
- **Private:** nothing leaves your computer except the card Discord shows. No accounts, no analytics.

## Install

Installing takes two steps: the **app** (shares your music with Discord) and the **browser
extension** (tells the app what's playing). Both are one-time.

### 1. The app

**Windows**: download `ytm-beacon_windows_amd64.zip` from the
[Releases page](https://github.com/dash1101/ytm-beacon/releases/latest), unzip it, and double-click
`ytm-beacon.exe`. Press Enter when asked. Or, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/install.ps1 | iex
```

**macOS and Linux**: paste this into a terminal:

```bash
curl -fsSL https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/install.sh | sh
```

(On macOS, prefer this command over downloading in a browser: it avoids Gatekeeper's
"unidentified developer" warning.)

The installer copies the app to a permanent place, unpacks the extension, sets the app to start at
login, and starts it. It needs no administrator rights and changes nothing system-wide.
Run `ytm-beacon uninstall` to undo all of it.

### 2. The browser extension

The installer prints the exact folder; you can print it again any time with `ytm-beacon extension`.

**Chrome, Brave, Edge, Opera, Vivaldi:**

1. Open `chrome://extensions` (Brave: `brave://extensions`, Edge: `edge://extensions`).
2. Turn on **Developer mode**.
3. Click **Load unpacked** and choose the `chrome` folder the installer showed you.

**Firefox:** install `ytm-beacon-firefox.xpi` from the Releases page (permanent, once signed
releases are published). To just try it, open `about:debugging#/runtime/this-firefox`, click
**Load Temporary Add-on...** and pick `manifest.json` from the `firefox` folder (Firefox forgets
temporary add-ons when it restarts).

### 3. Play something

Open [music.youtube.com](https://music.youtube.com), play a song, and look at your Discord
profile. Click the extension's toolbar icon any time for a quick "is everything connected?" check.

> **You won't see your own button.** Discord shows your "Listen on YouTube Music" button to
> other people, not to you. Ask a friend, or look at your profile from another account.

## Settings

Open the dashboard at **http://127.0.0.1:41713/** (also linked from the extension popup) to:

- show or hide the button and the album art,
- choose whether to keep the card visible while paused, and for how long,
- choose what Discord shows next to your name in the member list (*"Listening to YouTube Music"*,
  the song title, or the artist),
- send a test song to Discord without opening YouTube Music,
- copy diagnostics for a bug report.

Settings are saved in `config.json` (its location is shown on the dashboard and by
`ytm-beacon doctor`).

## Troubleshooting

**Step one, always:**

```bash
ytm-beacon doctor
```

It checks that the app is running, that Discord is reachable, and that the extension is reporting
music, and it prints a fix for whatever is wrong.

| What you see | What to do |
| --- | --- |
| *"The app is not running"* | Run `ytm-beacon install` (sets it up and starts it). |
| *"Cannot reach Discord"* | Open the Discord **desktop** app (the website can't do Rich Presence). The app reconnects within seconds. Using Flatpak/Snap Discord? See the [Linux notes](docs/TROUBLESHOOTING.md#linux-flatpak-and-snap-discord). |
| *"The browser extension has never reported in"* | Make sure the extension is installed and enabled, then reload your YouTube Music tab. |
| Everything is green but nothing shows | In Discord: **User Settings > Activity Privacy**, make sure sharing your activity is on, and that you aren't set to *Invisible*. |
| The icon in the toolbar shows a red **!** | The extension can't reach the app. Run `ytm-beacon doctor`. |

More cases (Firefox, antivirus, ports, wrong progress bar...) are in
[docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md). Still stuck?
[Open an issue](https://github.com/dash1101/ytm-beacon/issues/new/choose) and paste the output of
`ytm-beacon doctor`; it contains everything needed to help you.

## How it works

```
 YouTube Music tab         YTM Beacon app            Discord
 (browser extension)       (runs in background)      (desktop app)
┌───────────────────┐    ┌──────────────────┐    ┌──────────────┐
│ reads the song,   │───▶│ picks the active │───▶│ shows your   │
│ time and artwork  │HTTP│ tab, builds the  │IPC │ Rich Presence│
└───────────────────┘    │ card, retries    │    └──────────────┘
                         └──────────────────┘
                  127.0.0.1 only         local socket / named pipe
```

A web page can't talk to Discord directly, so a tiny app on your computer does. The extension
reads what YouTube Music is playing and sends it to the app over `127.0.0.1` (your own computer
only). The app talks to the Discord desktop app through the same local socket (a named pipe on
Windows) that games use for Rich Presence. No Discord password or token is involved.

## Using your own Discord app

The released builds come with a Discord application built in, so you don't need to do anything.
If you build from source or want your own name and images:

1. Go to <https://discord.com/developers/applications> and click **New Application**.
   The name you pick is what appears after "Listening to" (for example *YouTube Music*).
2. Copy the **Application ID** from the *General Information* page.
3. (Optional) Under **Rich Presence > Art Assets**, upload [`assets/logo.png`](assets/logo.png),
   [`assets/play.png`](assets/play.png) and [`assets/pause.png`](assets/pause.png) named exactly
   `logo`, `play` and `pause`. They provide the fallback artwork and the little play/pause badge.
4. Paste the Application ID into the dashboard (Settings), or set it in `config.json`, or set the
   `YTM_BEACON_CLIENT_ID` environment variable.

## Updating and uninstalling

- **Update:** run the install command again. It stops the old version, replaces it and restarts.
  Then reload the extension on your browser's extensions page.
- **Uninstall:** `ytm-beacon uninstall` (add `--purge` to also delete settings and logs), then
  remove the extension from your browser.

## Privacy and safety

- The app listens only on `127.0.0.1`. It refuses requests from websites (it checks the `Origin`
  and `Host` headers), so no web page can read your status or fake what you're listening to.
- The extension only runs on `music.youtube.com` and only talks to `127.0.0.1`.
- The song title, artist, album and album art URL go to Discord, because that is the card. Discord's
  servers fetch the artwork themselves.
- There is no telemetry, update check or network access of any kind besides that.

## Contributing

Bug reports and pull requests are welcome. See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for how
the code is organised, how to run it against a fake Discord, and how releases are made.

## License

[MIT](LICENSE)
