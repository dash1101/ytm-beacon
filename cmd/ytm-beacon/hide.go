package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dash1101/ytm-beacon/internal/config"
	"github.com/dash1101/ytm-beacon/internal/paths"
	"github.com/dash1101/ytm-beacon/internal/server"
)

// parseHideLength reads "30m", "2h", "1h30m" or a bare number of minutes.
func parseHideLength(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return time.Duration(n) * time.Minute, nil
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d, nil
	}
	return 0, fmt.Errorf("%q is not a length of time; use something like 30m, 2h or 90 (minutes)", s)
}

// describeLength says a duration the way a person would: "1 hour 30 minutes".
func describeLength(d time.Duration) string {
	m := int((d + time.Minute - 1) / time.Minute) // round up, so "1 second left" is "1 minute"
	if m < 1 {
		m = 1
	}
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s", unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch h, r := m/60, m%60; {
	case h == 0:
		return plural(r, "minute")
	case r == 0:
		return plural(h, "hour")
	default:
		return plural(h, "hour") + " " + plural(r, "minute")
	}
}

// cmdHide stops showing on Discord: for the given length of time, or until
// `ytm-beacon show` when no length is given.
func cmdHide(args []string) error {
	var d time.Duration
	if len(args) > 0 {
		var err error
		if d, err = parseHideLength(args[0]); err != nil {
			return err
		}
	}
	minutes := int((d + time.Minute - 1) / time.Minute)

	_, err := server.SetVisibility("hide", minutes)
	switch {
	case err == nil && minutes > 0:
		fmt.Printf("Hidden for %s. It comes back by itself, or run:  ytm-beacon show\n", describeLength(d))
	case err == nil:
		fmt.Println("Hidden. Nothing is shown on Discord until you run:  ytm-beacon show")
	case errors.Is(err, server.ErrNotRunning) && minutes == 0:
		if err := setEnabledInFile(false); err != nil {
			return err
		}
		fmt.Println("YTM Beacon isn't running right now. It will stay hidden when it starts.")
		fmt.Println("Turn it back on with:  ytm-beacon show")
	case errors.Is(err, server.ErrNotRunning):
		return errors.New("YTM Beacon is not running, so there is nothing to hide. (Without a time, `ytm-beacon hide` works even then.)")
	default:
		return err
	}
	return nil
}

// cmdShow turns showing on Discord back on.
func cmdShow() error {
	_, err := server.SetVisibility("show", 0)
	switch {
	case err == nil:
		fmt.Println("Showing on Discord again.")
	case errors.Is(err, server.ErrNotRunning):
		if err := setEnabledInFile(true); err != nil {
			return err
		}
		fmt.Println("YTM Beacon isn't running right now. It will show on Discord when it starts.")
	default:
		return err
	}
	return nil
}

// setEnabledInFile changes the master switch in the settings file, for when
// the app isn't running to ask.
func setEnabledInFile(on bool) error {
	path, err := paths.ConfigFile()
	if err != nil {
		return err
	}
	store, err := config.Load(path)
	if err != nil {
		return err
	}
	return store.Update(func(c *config.Config) error {
		c.Enabled = on
		return nil
	})
}
