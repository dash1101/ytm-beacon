// Package beacon exists only to embed the browser extension into the app binary,
// so `ytm-beacon install` can unpack it without any separate download.
package beacon

import "embed"

// Extension holds the browser extension sources (the extension/ directory).
//
//go:embed all:extension
var Extension embed.FS
