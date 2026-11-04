package autostart

import (
	"fmt"
	"strings"
)

// These functions only build text, so they can be unit-tested on any OS.

func systemdQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, `%`, `%%`)
	return `"` + s + `"`
}

func systemdUnit(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=YTM Beacon - YouTube Music Rich Presence for Discord
After=graphical-session.target

[Service]
ExecStart=%s run
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, systemdQuote(exe))
}

func desktopEntry(exe string) string {
	q := strings.ReplaceAll(exe, `\`, `\\`)
	q = strings.ReplaceAll(q, `"`, `\"`)
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=YTM Beacon
Comment=YouTube Music Rich Presence for Discord
Exec="%s" run
Terminal=false
X-GNOME-Autostart-enabled=true
`, q)
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func launchdPlist(label, exe, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardErrorPath</key>
  <string>%s</string>
  <key>StandardOutPath</key>
  <string>%s</string>
</dict>
</plist>
`, xmlEscape(label), xmlEscape(exe), xmlEscape(logPath), xmlEscape(logPath))
}
