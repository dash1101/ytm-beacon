# Changelog

## 0.1.2

Stop showing, and more settings.

- **Stop showing on Discord.** A switch at the top of the dashboard and in the extension's toolbar popup,
  the keyboard shortcut `Alt+Shift+M`, and `ytm-beacon hide` / `ytm-beacon show`. Switch off until you
  turn it on again, or hide for a while (`ytm-beacon hide 1h`; 1 or 3 hours on the dashboard, 1 hour in
  the popup) and it comes back by itself. The toolbar icon shows **off** while hidden, and `doctor`
  reports it. The new `/v1/visibility` API is what they all use.
- **Private mode:** shows only "Listening to music" (no song, artist, art, link or progress).
- **Never show songs containing these words:** a list matched against title, artist and album.
- **Progress bar** and **play / pause icon** can each be turned off.
- Settings now **save by themselves** (no Save button), are grouped, and the status says *why* nothing
  is showing (switched off, hidden for a while, a "never show" word, paused too long).
- The installer waits for Discord's first connection instead of reporting "isn't open yet" too early.
- The extension build now carries a `commands` entry (the shortcut). No new permissions.

## 0.1.1

- The Chrome extension declares Chrome 111 as its minimum version.
- CI validates the Firefox package with Mozilla's add-on validator.

## 0.1.0

First version.

- Browser extension (Chrome-family and Firefox) that reads what YouTube Music is playing.
- Standalone app for Windows, macOS and Linux that shows it as Discord Rich Presence
  (Listening card with album art, progress bar and a "Listen on YouTube Music" button).
- `install` / `uninstall` with start-at-login, `doctor` self-check, local dashboard with settings.
- Automatic reconnect when Discord restarts; fallback to a simpler card for older Discord clients.
- Works when another extension replaces the page's media session (an in-page script relays the song).
- Guided first-run setup page that opens after install; the extension finds already-open tabs by itself.
- `ytm-beacon update` (verified download) and an extension that reloads itself after an app update.
- `ytm-beacon uninstall` (plus one-line uninstall scripts) and a privacy policy.
