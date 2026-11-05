# Privacy policy

YTM Beacon shows what you're playing on YouTube Music as your Discord status. It is built to keep
everything on your own computer.

## What it reads

The browser extension runs only on `https://music.youtube.com`. On that page it reads:

- the title, artist and album of the song that is playing,
- the address of its album art (and the video ID, to build the "Listen" link),
- whether it is playing or paused, and the playback position and length.

It does not read your account, your library, your history, your recommendations, your cookies, or
any other website.

## Where that information goes

1. **To the YTM Beacon app on your own computer**, over `http://127.0.0.1` (a connection that never
   leaves your machine).
2. **From the app to the Discord desktop app on your own computer**, through Discord's local
   Rich Presence connection. Discord then shows the card on your profile according to *your* Discord
   privacy settings, and Discord's own privacy policy applies to what it does with it. Discord's servers
   fetch the album art image themselves.

Nothing else is sent anywhere. There is no server run by this project, no account, no telemetry,
no analytics, no advertising and no crash reporting. The song information is held in memory only
and is not written to disk (the log file records only events such as "connected to Discord" and
errors, plus song titles when you run with `--debug`).

## What is stored

On your computer only:

- `config.json`: your settings (for example whether to show the button, and your list of
  "never show" words if you add any).
- A log file and an empty marker file that records that setup finished.
- The extension keeps one small note in the browser's local extension storage (which version it last
  reloaded itself for), so that it never reloads in a loop.

`ytm-beacon uninstall --purge` deletes all of these.

## Network access

The app and extension make no internet connections by themselves. The one exception is the
`ytm-beacon update` command, which you run yourself: it contacts GitHub to download the latest
release and its checksum file.

## Permissions the extension asks for, and why

| Permission | Why |
| --- | --- |
| Access to `music.youtube.com` | To read what is playing on that page. |
| Access to `127.0.0.1` | To hand it to the YTM Beacon app on your computer. |
| `scripting` | To start the extension in a YouTube Music tab that was already open when it was installed, so you don't have to reload the tab. |
| `storage` | To remember which version it last reloaded for (see above). |

## Contact

Questions or concerns: open an issue at <https://github.com/dash1101/ytm-beacon/issues>.
