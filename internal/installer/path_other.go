//go:build !windows

package installer

// PATH editing is only done on Windows; see ensureOnPath.
func addToPath(string) error { return nil }

func removeFromPath(string) {}
