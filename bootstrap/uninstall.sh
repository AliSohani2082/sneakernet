#!/bin/sh
# Remove Sneakernet:   sudo sh uninstall.sh
# Same as running `sudo sneakernet uninstall` on the installed system.
set -eu

if [ "$(id -u)" -ne 0 ]; then
    exec sudo sh "$0" "$@"
fi

for bin in /opt/sneakernet/bin/sneakernet "$(command -v sneakernet 2>/dev/null || true)"; do
    if [ -n "$bin" ] && [ -x "$bin" ]; then
        # The binary deletes itself; run a copy so that is safe.
        tmp=$(mktemp -d)
        trap 'rm -rf "$tmp"' EXIT INT TERM
        cp "$bin" "$tmp/sneakernet"
        "$tmp/sneakernet" uninstall "$@"
        exit $?
    fi
done
echo "Sneakernet does not seem to be installed (no /opt/sneakernet/bin/sneakernet)." >&2
exit 1
