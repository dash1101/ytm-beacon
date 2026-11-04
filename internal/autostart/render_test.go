package autostart

import (
	"strings"
	"testing"
)

func TestSystemdUnit(t *testing.T) {
	u := systemdUnit(`/home/a b/.local/bin/ytm-beacon`)
	for _, want := range []string{
		`ExecStart="/home/a b/.local/bin/ytm-beacon" run`,
		"WantedBy=default.target",
		"Restart=on-failure",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit is missing %q:\n%s", want, u)
		}
	}
	if q := systemdQuote(`/tmp/100%/x"y`); q != `"/tmp/100%%/x\"y"` {
		t.Errorf("quote = %s", q)
	}
}

func TestDesktopEntry(t *testing.T) {
	d := desktopEntry("/home/u/.local/bin/ytm-beacon")
	if !strings.Contains(d, `Exec="/home/u/.local/bin/ytm-beacon" run`) || !strings.Contains(d, "Type=Application") {
		t.Errorf("bad entry:\n%s", d)
	}
}

func TestLaunchdPlist(t *testing.T) {
	p := launchdPlist("dev.ytm-beacon", "/Users/a&b/.local/bin/ytm-beacon", "/tmp/l.log")
	for _, want := range []string{
		"<string>dev.ytm-beacon</string>",
		"<string>/Users/a&amp;b/.local/bin/ytm-beacon</string>",
		"<string>run</string>",
		"<key>RunAtLoad</key>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist is missing %q:\n%s", want, p)
		}
	}
}
