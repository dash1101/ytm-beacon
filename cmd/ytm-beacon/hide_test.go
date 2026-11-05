package main

import (
	"testing"
	"time"
)

func TestParseHideLength(t *testing.T) {
	good := map[string]time.Duration{
		"30m":   30 * time.Minute,
		"2h":    2 * time.Hour,
		"1h30m": 90 * time.Minute,
		"90":    90 * time.Minute, // a bare number is minutes
		" 15 ":  15 * time.Minute,
	}
	for in, want := range good {
		if got, err := parseHideLength(in); err != nil || got != want {
			t.Errorf("%q: got %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "soon", "0", "-5", "-1h", "0m"} {
		if _, err := parseHideLength(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
}

func TestDescribeLength(t *testing.T) {
	cases := map[time.Duration]string{
		time.Second:                     "1 minute",
		30 * time.Minute:                "30 minutes",
		time.Hour:                       "1 hour",
		2 * time.Hour:                   "2 hours",
		90 * time.Minute:                "1 hour 30 minutes",
		2*time.Hour + 1*time.Minute:     "2 hours 1 minute",
		59*time.Minute + 30*time.Second: "1 hour", // rounds up
	}
	for d, want := range cases {
		if got := describeLength(d); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}
