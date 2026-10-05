#!/bin/sh
# Sneakernet installer. Run it from the USB stick:   sudo sh install.sh
#
# POSIX sh only. The stick is exFAT, which has no execute permission, so
# this script copies the right binary for this CPU somewhere executable and
# runs it from there. Extra arguments are passed on (see: sh install.sh -h).
set -eu

BUNDLE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
        exec sudo sh "$BUNDLE/install.sh" "$@"
    elif command -v pkexec >/dev/null 2>&1; then
        exec pkexec sh "$BUNDLE/install.sh" "$@"
    fi
    echo "Please run this as root:  su -c 'sh $BUNDLE/install.sh'" >&2
    exit 1
fi

case "$(uname -m)" in
    x86_64|amd64)        ARCH=amd64 ;;
    aarch64|arm64)       ARCH=arm64 ;;
    i386|i486|i586|i686) ARCH=386 ;;
    armv7*|armv8l)       ARCH=armv7 ;;
    *) echo "Sorry, this CPU ($(uname -m)) is not supported." >&2; exit 1 ;;
esac

SRC="$BUNDLE/bin/$ARCH/sneakernet"
if [ ! -f "$SRC" ]; then
    echo "This stick has no Sneakernet build for $ARCH ($SRC is missing)." >&2
    exit 1
fi

# Find a temp dir we may execute from (/tmp is noexec on some systems).
WORK=""
for d in "${TMPDIR:-/tmp}" /tmp /var/tmp /run /dev/shm; do
    [ -d "$d" ] || continue
    w=$(mktemp -d "$d/sneakernet.XXXXXX" 2>/dev/null) || continue
    if cp "$SRC" "$w/sneakernet" && chmod 0755 "$w/sneakernet" && "$w/sneakernet" version >/dev/null 2>&1; then
        WORK=$w
        break
    fi
    rm -rf "$w"
done
if [ -z "$WORK" ]; then
    echo "Could not run the installer from any temp directory (all noexec?)." >&2
    exit 1
fi
trap 'rm -rf "$WORK"' EXIT INT TERM

# Not exec: the trap must clean up the temp copy afterwards.
"$WORK/sneakernet" install --bundle "$BUNDLE" --arch "$ARCH" "$@"
