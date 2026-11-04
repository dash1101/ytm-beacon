// Package version holds build-time information.
package version

// Set at build time with -ldflags "-X github.com/dash1101/ytm-beacon/internal/version.Version=1.2.3".
var (
	Version = "dev"
	Commit  = "none"
)

// RepoURL is where users go for downloads, docs and bug reports.
const RepoURL = "https://github.com/dash1101/ytm-beacon"
