#!/bin/sh
# v2ray-kit bootstrap. POSIX sh only: runs from exFAT (no exec bits) via `sh install.sh`.
# Detects the CPU arch, copies the matching v2kit binary to a temp dir and execs it.
set -eu

BUNDLE_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        exec sudo sh "$BUNDLE_DIR/install.sh" "$@"
    elif command -v pkexec >/dev/null 2>&1; then
        exec pkexec sh "$BUNDLE_DIR/install.sh" "$@"
    fi
    echo "error: run this script as root" >&2
    exit 1
fi

case "$(uname -m)" in
    x86_64|amd64)          ARCH=amd64 ;;
    aarch64|arm64)         ARCH=arm64 ;;
    i386|i486|i586|i686)   ARCH=386 ;;
    armv7l|armv7*)         ARCH=armv7 ;;
    *) echo "error: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

SRC="$BUNDLE_DIR/bin/$ARCH/v2kit"
if [ ! -f "$SRC" ]; then
    echo "error: no v2kit binary for $ARCH in $BUNDLE_DIR/bin" >&2
    exit 1
fi

WORK=$(mktemp -d "${TMPDIR:-/tmp}/v2kit.XXXXXX")
trap 'rm -rf "$WORK"' EXIT INT TERM
cp "$SRC" "$WORK/v2kit"
chmod 0755 "$WORK/v2kit"

# Not exec: the trap must run afterwards to clean up the temp copy.
"$WORK/v2kit" install --bundle "$BUNDLE_DIR" --arch "$ARCH" "$@"
