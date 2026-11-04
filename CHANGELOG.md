# Changelog

## Unreleased

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
