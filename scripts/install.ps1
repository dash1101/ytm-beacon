# YTM Beacon installer for Windows.
#
#   irm https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/install.ps1 | iex
#
# Downloads the latest release, checks its SHA-256 checksum, and runs
# `ytm-beacon install`. No administrator rights needed.
$ErrorActionPreference = 'Stop'
$Repo = 'dash1101/ytm-beacon'

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$file = "ytm-beacon_windows_$arch.zip"
$base = "https://github.com/$Repo/releases/latest/download"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ('ytm-beacon-' + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Write-Host "Downloading $file ..."
    Invoke-WebRequest "$base/$file" -OutFile (Join-Path $tmp $file) -UseBasicParsing
    Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match ([regex]::Escape($file) + '$') } | Select-Object -First 1
    if (-not $line) { throw "No checksum listed for $file." }
    $expected = ($line -split '\s+')[0].ToLower()
    $actual = (Get-FileHash (Join-Path $tmp $file) -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { throw "Checksum check FAILED for $file. Not installing." }
    Write-Host 'Checksum OK.'

    Expand-Archive (Join-Path $tmp $file) -DestinationPath (Join-Path $tmp 'app') -Force
    & (Join-Path $tmp 'app\ytm-beacon.exe') install
}
finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
