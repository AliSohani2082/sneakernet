#!/bin/sh
# Download the pinned upstream artifacts listed in versions.lock into
# .cache/deps and verify their checksums. Needs internet; run it once on a
# machine with access, then everything else works offline.
#
#   scripts/fetch-deps.sh          download what is missing, verify everything
#   scripts/fetch-deps.sh verify   verify only, never touch the network
#   scripts/fetch-deps.sh urls     print the download URLs (to fetch by hand,
#                                  then drop the files into .cache/deps)
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEPS="$ROOT/.cache/deps"
MODE="${1:-fetch}"
mkdir -p "$DEPS"

url() { # name version file
    case "$1" in
        xray) echo "https://github.com/XTLS/Xray-core/releases/download/$2/$3" ;;
        *) echo "unknown artifact $1" >&2; exit 1 ;;
    esac
}

if [ "$MODE" = fetch ] && ! command -v curl >/dev/null 2>&1; then
    echo "error: curl not found. Install it, or run '$0 urls', download those" >&2
    echo "       files by hand into .cache/deps and run '$0 verify'." >&2
    exit 1
fi

status=0
grep -vE '^[[:space:]]*(#|$)' "$ROOT/versions.lock" > "$DEPS/.lock"
while read -r name ver arch file sum; do
    case "$MODE" in
        urls) url "$name" "$ver" "$file"; continue ;;
        fetch)
            if [ ! -f "$DEPS/$file" ]; then
                echo "download $file ($arch)"
                curl -fL --retry 5 --retry-delay 3 -C - -o "$DEPS/$file.part" "$(url "$name" "$ver" "$file")" || {
                    echo "error: download of $file failed. If GitHub is blocked here, fetch the" >&2
                    echo "       URLs from '$0 urls' another way and put the files in .cache/deps." >&2
                    exit 1
                }
                mv "$DEPS/$file.part" "$DEPS/$file"
            fi ;;
        verify) ;;
        *) echo "usage: $0 [fetch|verify|urls]" >&2; exit 2 ;;
    esac
    if [ ! -f "$DEPS/$file" ]; then
        echo "MISSING   $file" >&2; status=1; continue
    fi
    got=$(sha256sum "$DEPS/$file" | cut -d' ' -f1)
    if [ "$got" != "$sum" ]; then
        echo "MISMATCH  $file (got $got)" >&2; status=1; continue
    fi
    echo "ok        $file"
done < "$DEPS/.lock"
rm -f "$DEPS/.lock"
if [ "$status" -ne 0 ] && [ "$MODE" != urls ]; then
    echo "Some files are missing or damaged. Delete the bad ones from .cache/deps and run 'make fetch' again." >&2
fi
exit "$status"
