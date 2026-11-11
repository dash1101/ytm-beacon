<p align="center"><img src="assets/logo.png" width="112" alt=""></p>

# YTM Beacon

> **YTM Beacon is now part of [YTM Starcast](https://github.com/dash1101/ytm-starcast).** YTM Starcast shows YouTube
> Music as a Discord status just like YTM Beacon, and adds a phone remote for YouTube Music and improvements inside
> YouTube Music, each with its own switch. Its installer brings YTM Beacon's settings along and removes the old app.
> YTM Starcast needs a Chromium-based browser (Chrome, Brave, Edge…) on Linux or Windows; in Firefox or on macOS, YTM
> Beacon keeps working as it is. This repository is archived: existing installs and releases keep working, but there
> will be no new ones.

Show what you're listening to on **YouTube Music** as your **Discord** status: song, artist,
album art, a live progress bar, and a "Listen on YouTube Music" button.

- **Works on Windows, macOS and Linux**, with Chrome, Brave, Edge, Opera, Vivaldi and Firefox.
- **One small download, nothing else to install.** No Node, no Python, no Discord login.
- **Starts by itself** when you log in and **reconnects by itself** when you restart Discord.
- **Updates with one command**, and the browser extension reloads itself.
- **Easy to debug:** `ytm-beacon doctor` checks everything and tells you what to fix.
- **Stop showing in one click:** a switch on the dashboard and the toolbar icon, a keyboard shortcut
  (`Alt+Shift+M`), or `ytm-beacon hide`. Hide for an hour, or until you turn it back on.
- **Private:** nothing leaves your computer except the card Discord shows. No accounts, no analytics.

## Quick start

**1. Install the app** (paste into a terminal, or on Windows just download, unzip and double-click):

| System | Do this |
| --- | --- |
| **Linux / macOS** | `curl -fsSL https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/install.sh \| sh` |
| **Windows** (PowerShell) | `irm https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/install.ps1 \| iex` |
| **Windows** (no terminal) | Download `ytm-beacon_windows_amd64.zip` from the [Releases page](https://github.com/dash1101/ytm-beacon/releases/latest), unzip it, double-click `ytm-beacon.exe`. |

It installs for your user only (no administrator rights), starts YTM Beacon, and sets it to start
at login.

**2. Add the browser extension** (one time). A **setup page opens in your browser** with the exact
steps for *your* browser and the folder to pick, each with a Copy button. It takes three clicks:

1. Open your browser's extensions page (`chrome://extensions`, `brave://extensions`, `edge://extensions`...).
2. Turn on **Developer mode**.
3. Click **Load unpacked** and choose the folder shown on the setup page.

Firefox: the setup page shows the Firefox steps (a permanent, signed `.xpi` is on the Releases page).

**3. Play a song** on [music.youtube.com](https://music.youtube.com). Look at your Discord profile.
The setup box disappears by itself once YouTube Music is detected.

> **You won't see your own button.** Discord shows the "Listen on YouTube Music" button to other
> people, not to you.

## Updating

```bash
ytm-beacon update
```

It finds the latest release, downloads it, **verifies its SHA-256 checksum**, and installs it. The
browser extension notices the new version and **reloads itself within seconds**, so there is nothing
to do in your browser. Use `ytm-beacon update --check` to just see whether a newer version exists.
It only contacts the network when you run this command.

You can also re-run the install command from the Quick start; it does the same.

## Uninstalling

```bash
ytm-beacon uninstall
```

That stops the app, removes start-at-login and deletes the program and its copy of the extension.
Your settings are kept in case you come back; add `--purge` to delete those as well:

```bash
ytm-beacon uninstall --purge
```

Then remove the extension from your browser (this part can only be done in the browser): open the
extensions page, find **YTM Beacon**, click **Remove**. After that nothing of YTM Beacon is left.

Can't run `ytm-beacon`? Use the one-line uninstallers (they find the program wherever it was installed):

| System | Do this |
| --- | --- |
| **Linux / macOS** | `curl -fsSL https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/uninstall.sh \| sh` |
| **Windows** (PowerShell) | `irm https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/uninstall.ps1 \| iex` |

<details>
<summary>Removing everything by hand</summary>

**Linux**

```bash
systemctl --user disable --now ytm-beacon.service
rm -f ~/.config/systemd/user/ytm-beacon.service ~/.config/autostart/ytm-beacon.desktop
rm -rf ~/.local/bin/ytm-beacon ~/.local/share/ytm-beacon ~/.config/ytm-beacon
```

**macOS**

```bash
launchctl bootout gui/$(id -u)/dev.ytm-beacon
rm -f ~/Library/LaunchAgents/dev.ytm-beacon.plist ~/.local/bin/ytm-beacon
rm -rf "$HOME/Library/Application Support/ytm-beacon"
```

**Windows** (PowerShell)

```powershell
Stop-Process -Name ytm-beacon-bg, ytm-beacon -ErrorAction SilentlyContinue
reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v YTMBeacon /f
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\ytm-beacon", "$env:APPDATA\ytm-beacon"
```

</details>

## Settings

Open the dashboard at **http://127.0.0.1:41713/** (also linked from the extension popup). Changes save
by themselves.

**Stop showing (the switch at the top):** hide your music from Discord whenever you like. It's
also on the extension's toolbar icon, on the keyboard (`Alt+Shift+M` toggles it; change the key on your
browser's extension shortcuts page) and in a terminal:

```bash
ytm-beacon hide        # switch off until you run "show"
ytm-beacon hide 1h     # hide for a while (30m, 2h, 90...); it comes back by itself
ytm-beacon show        # back on
```

While it's off the toolbar icon turns grey and says **off**. The app and extension keep running, so turning it on
again is instant. "Switch off" is remembered across restarts; a timed hide is not.

**What your card shows:**

- album art, progress bar and time, the small play / pause icon, and the "Listen on YouTube Music" button,
- **Show when paused:** on, your song stays on your profile with a pause icon next to it (it goes away
  after the number of minutes you choose, or never with 0); off, your card disappears completely the
  moment you pause. The extension's toolbar popup has the same switch,
- what Discord shows next to your name in the member list (*"Listening to YouTube Music"*, the song
  title, or the artist).

**Privacy:**

- **Private mode** shows only *"Listening to music"*: no song, artist, album art or link.
- **Never show songs containing these words**: a list (one per line) matched against the title,
  artist and album, ignoring capitals. Matching songs just don't appear.

**Also:** send a test song to Discord without opening YouTube Music, copy diagnostics for a bug
report, or use your own Discord application ID.

Settings are saved in `config.json` (its location is shown on the dashboard and by
`ytm-beacon doctor`).

## Troubleshooting

**Step one, always:**

```bash
ytm-beacon doctor
```

It checks that the app is running, that Discord is reachable, and that the extension is reporting
music, and it prints a fix for whatever is wrong. The extension's toolbar popup does a similar
check and says what it sees in your YouTube Music tab.

| What you see | What to do |
| --- | --- |
| *"The app is not running"* | Run `ytm-beacon install` (sets it up and starts it). |
| *"Cannot reach Discord"* | Open the Discord **desktop** app (the website can't do Rich Presence). The app reconnects within seconds. Using Flatpak/Snap Discord? See the [Linux notes](docs/TROUBLESHOOTING.md#linux-flatpak-and-snap-discord). |
| *"The browser extension has never reported in"* | Make sure the extension is installed and enabled, then click its toolbar icon: it says what's wrong in your YouTube Music tab. |
| *"Nothing is playing yet"* but a song is playing | Another extension (an ad blocker, a YouTube enhancer) may be interfering. YTM Beacon works around the known cases; see the [guide](docs/TROUBLESHOOTING.md#the-extension-isnt-reporting). |
| Everything is green but nothing shows | Is the toolbar icon marked **off**? You (or the keyboard shortcut) switched it off: run `ytm-beacon show`. Otherwise, in Discord: **User Settings > Activity Privacy**, make sure sharing your activity is on, and that you aren't set to *Invisible*. |
| The toolbar icon shows a red **!** | The extension can't reach the app. Run `ytm-beacon doctor`. |

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
3. Paste the Application ID into the dashboard (Settings), or set it in `config.json`, or set the
   `YTM_BEACON_CLIENT_ID` environment variable.

There is nothing to upload to the application: the logo and the little play / pause badge are sent
to Discord as web addresses of the images in this repository's [`assets/`](assets) folder.

## Privacy and safety

See the full [privacy policy](PRIVACY.md). In short:

- The app listens only on `127.0.0.1`. It refuses requests from websites (it checks the `Origin`
  and `Host` headers), so no web page can read your status or fake what you're listening to.
- The extension only runs on `music.youtube.com` and only talks to `127.0.0.1`.
- The song title, artist, album and album art URL go to Discord, because that is the card. Discord's
  servers fetch the artwork (and the small logo and play / pause icons, which live in this
  repository's `assets/` folder) themselves.
- There is no telemetry and no automatic update check. The only time the program uses the internet
  is when you run `ytm-beacon update`.

## Contributing

Bug reports and pull requests are welcome. See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for how
the code is organised, how to run it against a fake Discord, and how releases are made.

## License

[MIT](LICENSE)
