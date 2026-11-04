#!/bin/sh
# YTM Beacon installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/install.sh | sh
#
# Downloads the latest release for your system, checks its SHA-256 checksum,
# and runs `ytm-beacon install`. Nothing is installed system-wide and no
# administrator rights are needed.
set -eu

REPO="dash1101/ytm-beacon"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux|darwin) ;;
  *) echo "Unsupported system: $os. On Windows use install.ps1 (see the README)." >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "Unsupported CPU: $arch" >&2; exit 1 ;;
esac

file="ytm-beacon_${os}_${arch}.tar.gz"
# YTM_BEACON_BASE_URL lets developers test this script against a local folder.
base="${YTM_BEACON_BASE_URL:-https://github.com/${REPO}/releases/latest/download}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $file ..."
curl -fsSL "$base/$file" -o "$tmp/$file"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

expected="$(grep " $file\$" "$tmp/checksums.txt" | awk '{print $1}')"
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$file" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$file" | awk '{print $1}')"
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "Checksum check FAILED for $file. Not installing." >&2
  exit 1
fi
echo "Checksum OK."

tar -xzf "$tmp/$file" -C "$tmp"
"$tmp/ytm-beacon" install
