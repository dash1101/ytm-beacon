//go:build !windows

package installer

// scheduleDelete is only needed on Windows, where a running program can't
// delete its own file. Elsewhere os.Remove works even while it runs.
func scheduleDelete(string) {}
