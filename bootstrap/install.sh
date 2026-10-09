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

# The sha256 SHA256SUMS lists for this CPU's binary. This catches a damaged or
# half-copied stick before anything runs as root. It does NOT stop a malicious
# stick: SHA256SUMS lives on the same media as the binary (see docs/SECURITY.md).
SUMS="$BUNDLE/SHA256SUMS"
if ! command -v sha256sum >/dev/null 2>&1; then
    echo "sha256sum is required to check the stick before running it (coreutils)." >&2
    exit 1
fi
[ -f "$SUMS" ] || { echo "This is not a complete Sneakernet folder: SHA256SUMS is missing." >&2; exit 1; }
WANT=$(awk -v p="bin/$ARCH/sneakernet" '$2 == p || $2 == "*" p { print $1; exit }' "$SUMS")
case "$WANT" in
    *[!0-9a-f]*|"") echo "SHA256SUMS has no valid entry for bin/$ARCH/sneakernet; re-copy the folder to the stick." >&2; exit 1 ;;
esac
[ "${#WANT}" -eq 64 ] || { echo "SHA256SUMS has a malformed entry for bin/$ARCH/sneakernet." >&2; exit 1; }

# Find a temp dir we may execute from (/tmp is noexec on some systems). The
# checksum is taken from the private copy that is then executed, not from the
# stick, so the stick cannot change between the check and the run.
WORK=""
for d in "${TMPDIR:-/tmp}" /tmp /var/tmp /run /dev/shm; do
    [ -d "$d" ] || continue
    w=$(mktemp -d "$d/sneakernet.XXXXXX" 2>/dev/null) || continue
    if cp "$SRC" "$w/sneakernet" && chmod 0755 "$w/sneakernet"; then
        GOT=$(sha256sum "$w/sneakernet" | awk '{ print $1 }')
        if [ "$GOT" != "$WANT" ]; then
            rm -rf "$w"
            echo "bin/$ARCH/sneakernet does not match SHA256SUMS: the stick is damaged. Re-copy the folder and try again." >&2
            exit 1
        fi
        if "$w/sneakernet" version >/dev/null 2>&1; then
            WORK=$w
            TMPDIR=$d   # staged files are executed too, so use a dir that allows it
            export TMPDIR
            break
        fi
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
