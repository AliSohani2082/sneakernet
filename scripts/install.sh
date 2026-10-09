#!/bin/sh
# Put Sneakernet on a Ventoy USB stick (as <stick>/sneakernet/).
#
# Online, from a published release:
#   curl -fsSL https://raw.githubusercontent.com/AliSohani2082/sneakernet/main/scripts/install.sh | sh
# Offline, from a bundle you built (make bundle) or downloaded earlier:
#   sh scripts/install.sh --from dist/sneakernet
#   sh scripts/install.sh --from sneakernet.tar.gz
#
# This only writes to the stick; it never uses sudo. To connect a computer,
# boot it from the stick and run `sudo sh <stick>/sneakernet/install.sh`.
#
# Every file is checked: the release archive against its .sha256 (or the
# --sha256 you pass), then the copy on the stick against SHA256SUMS. A
# servers.txt that is already on the stick is kept.
set -eu

REPO_URL="${SNEAKERNET_URL:-https://github.com/AliSohani2082/sneakernet/releases}"
ARCHIVE=sneakernet.tar.gz

usage() {
    cat <<EOF
Usage: sh install.sh [--to STICK] [--from DIR|FILE] [--version TAG] [--sha256 HEX]

  --to STICK      mounted Ventoy partition (default: found automatically)
  --from SOURCE   a bundle folder (dist/sneakernet) or a sneakernet.tar.gz;
                  nothing is downloaded. Default: download the release.
  --version TAG   release to download, e.g. v1.0.0 (default: latest)
  --sha256 HEX    expected sha256 of the downloaded archive (recommended:
                  copy it from the release page you trust)

  SNEAKERNET_URL  releases base URL (default: $REPO_URL)
EOF
}

die() { printf 'error: %s\n' "$*" >&2; exit 1; }
say() { printf '%s\n' "$*"; }

to="" from="" version="" want_sum=""
while [ $# -gt 0 ]; do
    case "$1" in
        --to)      [ $# -ge 2 ] || die "--to needs a directory"; to=$2; shift 2 ;;
        --from)    [ $# -ge 2 ] || die "--from needs a path"; from=$2; shift 2 ;;
        --version) [ $# -ge 2 ] || die "--version needs a tag"; version=$2; shift 2 ;;
        --sha256)  [ $# -ge 2 ] || die "--sha256 needs a checksum"; want_sum=$2; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; die "unknown argument: $1" ;;
    esac
done

sha256() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        die "need sha256sum or shasum to verify files"
    fi
}

# verify_bundle DIR: check every file listed in DIR/SHA256SUMS.
verify_bundle() {
    [ -f "$1/SHA256SUMS" ] && [ -f "$1/install.sh" ] ||
        die "$1 is not a Sneakernet bundle (no SHA256SUMS or install.sh)"
    bad=0 n=0
    while read -r sum path; do
        n=$((n + 1))
        if [ ! -f "$1/$path" ]; then
            printf 'MISSING   %s\n' "$path" >&2; bad=1
        elif [ "$(sha256 "$1/$path")" != "$sum" ]; then
            printf 'MISMATCH  %s\n' "$path" >&2; bad=1
        fi
    done < "$1/SHA256SUMS"
    [ "$bad" -eq 0 ] || return 1
    say "verified $n files in $1"
}

# Find the stick: exactly one of the usual Ventoy mount points.
if [ -z "$to" ]; then
    user=${SUDO_USER:-${USER:-$(id -un)}}
    found=""
    for d in "/run/media/$user/Ventoy" "/media/$user/Ventoy" /media/Ventoy /mnt/ventoy /Volumes/Ventoy; do
        [ -d "$d" ] || continue
        [ -z "$found" ] || die "found more than one stick ($found, $d); pick one with --to"
        found=$d
    done
    [ -n "$found" ] || die "no mounted Ventoy stick found. Plug it in, open it in your file manager (or mount it), then rerun with --to /path/to/Ventoy"
    to=$found
fi
[ -d "$to" ] || die "$to is not a directory (is the stick mounted?)"
[ -w "$to" ] || die "cannot write to $to. Remount it as your user; this script does not use sudo"

work=$(mktemp -d "${TMPDIR:-/tmp}/sneakernet-get.XXXXXX")
stage="$to/.sneakernet.new"
trap 'rm -rf "$work" "$stage"' EXIT INT TERM

# Resolve the source to a bundle folder.
if [ -n "$from" ] && [ -d "$from" ]; then
    src=$from
else
    if [ -n "$from" ]; then
        [ -f "$from" ] || die "$from does not exist"
        archive=$from
        sumfile="$from.sha256"
    else
        if [ -n "$version" ]; then base="$REPO_URL/download/$version"; else base="$REPO_URL/latest/download"; fi
        archive="$work/$ARCHIVE"
        sumfile="$archive.sha256"
        if command -v curl >/dev/null 2>&1; then
            get() { curl -fL --retry 3 -o "$2" "$1"; }
        elif command -v wget >/dev/null 2>&1; then
            get() { wget -O "$2" "$1"; }
        else
            die "need curl or wget to download (or use --from with a local bundle)"
        fi
        say "download $base/$ARCHIVE"
        get "$base/$ARCHIVE" "$archive" ||
            die "download failed. Is a release published at $REPO_URL? Otherwise build one: git clone + make bundle, then --from dist/sneakernet"
        if [ -z "$want_sum" ]; then
            get "$base/$ARCHIVE.sha256" "$sumfile" >/dev/null 2>&1 ||
                die "could not download $ARCHIVE.sha256; pass the checksum with --sha256"
        fi
    fi
    if [ -z "$want_sum" ]; then
        [ -f "$sumfile" ] || die "no $sumfile next to the archive; pass the checksum with --sha256"
        want_sum=$(cut -d' ' -f1 < "$sumfile")
    fi
    want_sum=$(printf %s "$want_sum" | tr "A-F" "a-f")
    got=$(sha256 "$archive")
    [ "$got" = "$want_sum" ] || die "checksum mismatch for $archive
  expected $want_sum
  got      $got"
    say "archive checksum ok"
    tar -xzf "$archive" -C "$work" || die "could not unpack $archive"
    src="$work/sneakernet"
fi

verify_bundle "$src" || die "$src is damaged; rebuild or download it again"

# Copy to a temp folder on the stick, check the copy, then swap it in.
say "copy to $to/sneakernet"
rm -rf "$stage"
cp -R "$src" "$stage"
verify_bundle "$stage" || die "the copy on the stick does not match. The stick may be failing; try another one"
if [ -f "$to/sneakernet/servers.txt" ]; then
    cp "$to/sneakernet/servers.txt" "$stage/servers.txt"
    say "kept your servers.txt"
fi
rm -rf "$to/sneakernet"
mv "$stage" "$to/sneakernet"
sync

say ""
say "Sneakernet $(cat "$to/sneakernet/VERSION" 2>/dev/null || echo '?') is on the stick: $to/sneakernet"
say "Edit $to/sneakernet/servers.txt to add your servers (one link per line)."
say "Then boot a live ISO from the stick and run:  sudo sh <stick>/sneakernet/install.sh"
