#!/usr/bin/env bash
# Builds everything a release needs into ./dist:
#   ytm-beacon_<os>_<arch>.tar.gz / .zip   the app for each platform (no dependencies)
#   ytm-beacon-chrome.zip / -firefox.zip   the browser extension
#   checksums.txt                          SHA-256 of all of the above
#   ext/{chrome,firefox}/                  unpacked extension (used for store signing)
#
# Usage: scripts/build-release.sh [version]     (default: $GITHUB_REF_NAME or "dev")
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-${GITHUB_REF_NAME:-dev}}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
PKG="github.com/dash1101/ytm-beacon/internal"
LDFLAGS="-s -w -X $PKG/version.Version=$VERSION -X $PKG/version.Commit=$COMMIT"
if [ -n "${DISCORD_CLIENT_ID:-}" ]; then
  LDFLAGS="$LDFLAGS -X $PKG/config.DefaultClientID=$DISCORD_CLIENT_ID"
fi
TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"

rm -rf dist
mkdir -p dist/stage

for target in $TARGETS; do
  os="${target%/*}"; arch="${target#*/}"
  name="ytm-beacon_${os}_${arch}"
  dir="dist/stage/$name"
  exe="ytm-beacon"; [ "$os" = "windows" ] && exe="ytm-beacon.exe"
  mkdir -p "$dir"
  echo "==> $name"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$dir/$exe" ./cmd/ytm-beacon
  if [ "$os" = "windows" ]; then
    # Same program without a console window; this is what starts at login.
    GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS -H=windowsgui" -o "$dir/ytm-beacon-bg.exe" ./cmd/ytm-beacon
  fi
  cp README.md LICENSE "$dir/"
  if [ "$os" = "windows" ]; then
    (cd "$dir" && zip -q -r "../../$name.zip" .)
  else
    tar -C "$dir" -czf "dist/$name.tar.gz" .
  fi
done

echo "==> browser extension"
go run -trimpath -ldflags "$LDFLAGS" ./cmd/ytm-beacon extension --out dist --zip
go run -trimpath -ldflags "$LDFLAGS" ./cmd/ytm-beacon extension --out dist/ext

rm -rf dist/stage
cd dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum *.tar.gz *.zip > checksums.txt
else
  shasum -a 256 *.tar.gz *.zip > checksums.txt
fi
echo
echo "Done. Contents of dist/:"
ls -1
