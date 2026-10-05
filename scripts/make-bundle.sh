#!/bin/sh
# Assemble the folder that goes on the Ventoy stick:
#
#   dist/sneakernet/
#     install.sh  uninstall.sh  README.txt  VERSION  SHA256SUMS
#     servers.txt                      the server list (users may edit it)
#     bin/<arch>/{sneakernet,xray}     amd64 arm64 386 armv7
#     data/{geoip.dat,geosite.dat}
#     licenses/
#
# Runs offline: it needs `make build` output in build/ and the verified
# artifacts from scripts/fetch-deps.sh in .cache/deps.
#
#   SERVERS=path/to/links.txt   server list to ship (default config/servers.txt)
#   ARCHES="amd64 arm64"        subset of architectures (default: all four)
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT="${1:-$ROOT/dist/sneakernet}"
DEPS="$ROOT/.cache/deps"
ARCHES="${ARCHES:-amd64 arm64 386 armv7}"
SERVERS="${SERVERS:-$ROOT/config/servers.txt}"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"

sh "$ROOT/scripts/fetch-deps.sh" verify >/dev/null

zip_for() { awk -v a="$1" '$1 == "xray" && $3 == a { print $4 }' "$ROOT/versions.lock"; }

rm -rf "$OUT"
mkdir -p "$OUT/data" "$OUT/licenses"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM

for arch in $ARCHES; do
    zip=$(zip_for "$arch")
    [ -n "$zip" ] || { echo "no Xray artifact pinned for $arch" >&2; exit 1; }
    [ -f "$ROOT/build/$arch/sneakernet" ] || { echo "missing build/$arch/sneakernet: run make build" >&2; exit 1; }
    mkdir -p "$OUT/bin/$arch" "$work/$arch"
    unzip -q -o "$DEPS/$zip" -d "$work/$arch"
    install -m 0755 "$work/$arch/xray" "$OUT/bin/$arch/xray"
    install -m 0755 "$ROOT/build/$arch/sneakernet" "$OUT/bin/$arch/sneakernet"
done

# Geo data and license are identical in every Xray zip; take them from the first.
first=$(echo "$ARCHES" | cut -d' ' -f1)
install -m 0644 "$work/$first/geoip.dat" "$work/$first/geosite.dat" "$OUT/data/"
install -m 0644 "$work/$first/LICENSE" "$OUT/licenses/Xray-core-LICENSE"
install -m 0644 "$ROOT/LICENSE" "$OUT/licenses/sneakernet-LICENSE" 2>/dev/null || true

if [ -f "$SERVERS" ]; then
    install -m 0644 "$SERVERS" "$OUT/servers.txt"
else
    # An empty list is fine: the installer and the TUI ask for links.
    echo "note: $SERVERS not found; shipping the commented example list" >&2
    install -m 0644 "$ROOT/config/servers.example.txt" "$OUT/servers.txt"
fi

install -m 0644 "$ROOT/bootstrap/install.sh" "$ROOT/bootstrap/uninstall.sh" "$ROOT/bootstrap/README.txt" "$OUT/"
echo "$VERSION" > "$OUT/VERSION"

# Checksums of the payload (paths relative to the bundle). servers.txt is
# left out on purpose: users edit it on the stick.
(cd "$OUT" && find bin data -type f | LC_ALL=C sort | xargs sha256sum > SHA256SUMS)

echo "bundle $VERSION -> $OUT"
du -sh "$OUT"
