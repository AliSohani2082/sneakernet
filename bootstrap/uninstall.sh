#!/bin/sh
# v2ray-kit uninstaller. Prefers the installed v2kit, falls back to the bundle copy.
set -eu

if [ "$(id -u)" -ne 0 ]; then
    exec sudo sh "$0" "$@"
fi

if command -v v2kit >/dev/null 2>&1; then
    exec v2kit uninstall "$@"
fi

BUNDLE_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec sh "$BUNDLE_DIR/install.sh" uninstall-only "$@"
