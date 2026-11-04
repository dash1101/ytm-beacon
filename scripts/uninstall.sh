#!/bin/sh
# Removes YTM Beacon on Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/dash1101/ytm-beacon/main/scripts/uninstall.sh | sh
#   (to also delete your settings:  ... | sh -s -- --purge)
#
# This just runs `ytm-beacon uninstall` from where the installer put it, so it
# works even if the command isn't on your PATH.
set -eu

BIN="${YTM_BEACON_HOME:+$YTM_BEACON_HOME/bin}"
BIN="${BIN:-$HOME/.local/bin}/ytm-beacon"

if [ -x "$BIN" ]; then
  exec "$BIN" uninstall "$@"
fi
if command -v ytm-beacon >/dev/null 2>&1; then
  exec ytm-beacon uninstall "$@"
fi
echo "YTM Beacon doesn't seem to be installed (looked for $BIN). Nothing to remove."
