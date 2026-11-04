//go:build windows

package installer

import (
	"fmt"
	"os/exec"
	"strings"
)

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func runPowerShell(script string) error {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// addToPath appends dir to the user's PATH (not the machine-wide one) if missing.
func addToPath(dir string) error {
	return runPowerShell(`$d = ` + psQuote(dir) + `;
$p = [Environment]::GetEnvironmentVariable('Path','User'); if (-not $p) { $p = '' };
if (($p -split ';') -notcontains $d) {
  [Environment]::SetEnvironmentVariable('Path', ($p.TrimEnd(';') + ';' + $d).TrimStart(';'), 'User')
}`)
}

func removeFromPath(dir string) {
	_ = runPowerShell(`$d = ` + psQuote(dir) + `;
$p = [Environment]::GetEnvironmentVariable('Path','User'); if (-not $p) { exit 0 };
$n = ($p -split ';' | Where-Object { $_ -and $_ -ne $d }) -join ';';
[Environment]::SetEnvironmentVariable('Path', $n, 'User')`)
}
