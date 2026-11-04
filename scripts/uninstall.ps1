# Removes YTM Beacon on Windows.
#
#   irm https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/uninstall.ps1 | iex
#
# This just runs `ytm-beacon uninstall` from where the installer put it, so it
# works even in a window where the command isn't on the PATH yet.
$exe = Join-Path $env:LOCALAPPDATA 'ytm-beacon\ytm-beacon.exe'
if (Test-Path $exe) {
    & $exe uninstall
} else {
    Write-Host "YTM Beacon doesn't seem to be installed (looked for $exe). Nothing to remove."
}
