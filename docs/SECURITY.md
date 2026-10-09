# Security notes

Sneakernet runs as root and installs a network proxy, so it is worth being
precise about what it does and does not protect against. Details of the review
this page responds to are in [reports/security-review.md](reports/security-review.md).

## What `SHA256SUMS` is, and is not

`SHA256SUMS` lists a checksum for every file the installer uses (the Xray and
Sneakernet binaries for your CPU, the geo data, `install.sh`, `uninstall.sh`).
Before anything runs as root, `install.sh` checks the Sneakernet binary against
it, and the installer checks everything it is about to install.

**This detects a damaged or half-copied stick. It does not detect a
malicious stick.** The checksums, the scripts and the binaries are all on the
same media. Someone who can write to the stick can change a binary *and* its
line in `SHA256SUMS` (and `install.sh` itself). Sneakernet has no signing key
and no trust anchor outside the stick, so it cannot tell those apart.

To get real protection today:

- Build the stick yourself (`make bundle`) from this repository and the pinned
  Xray archives (`versions.lock` hashes are verified when fetching).
- Keep the stick physically yours; do not plug it into untrusted machines
  between building and installing. Copy the folder to a Linux filesystem and
  run it from there if the stick may have been out of your sight.
- If you download a release tarball, check it against the checksum published
  *separately* (`--sha256` in `scripts/install.sh`).

Signed manifests with a key you hold are planned, not implemented.

## What the installer does to stay safe

- **Complete, strict manifest.** `SHA256SUMS` must list the exact binaries and
  data files for the selected CPU. Duplicate lines, absolute paths, `..`,
  backslashes and non-canonical paths are rejected. Payload files must be
  regular files; symlinks (also as a directory component) and devices/FIFOs
  are refused, and every read is size-limited.
- **Verify, then use the same bytes.** Files are copied into a fresh private
  (`0700`) directory, hashed *there*, and only those copies are installed. The
  Xray binary that validates the generated config is the staged copy, not
  the one on the stick, so swapping a file on the stick after the check has
  no effect.
- **No writes through symlinks.** Writes, renames, removals and `chmod`/`chown`
  to a target root other than `/` go through `os.Root`, so a symlink such as
  `<root>/etc -> /etc` is an error rather than a write to the host. Files are
  replaced by an atomic rename, so a symlink planted at the final name (for
  example the systemd unit) is replaced, not written through. For the running
  system (`/`) the host's own symlinks (usr-merge, NixOS) work as before.
- **Managed directories are checked.** `/opt/sneakernet{,/bin,/share}`,
  `/etc/sneakernet` and `/var/lib/sneakernet` must be real directories, owned by
  root and not writable by group or others. If one already exists differently
  (for example left behind by someone else), the install stops with a message
  naming it; fix the ownership/mode or remove the directory and run again.
- **The install log holds no secrets.** `/var/log/sneakernet-install.log` is
  opened without following symlinks and forced to `0600`. Pasted share links
  are never written to it, only that a line was read. Logs written by older
  versions may contain pasted links: delete `/var/log/sneakernet-install.log`
  if you pasted credentials into an earlier version.
- **Server names are sanitised for display.** ANSI/OSC escape sequences,
  control characters and bidi overrides in a link's name are dropped; the
  original link is kept unchanged in the server list.

## Known gaps

See the report for the full list. Not yet addressed: root-owned account
handling (S5), running probes and validation as an unprivileged user (S6),
the `0644` server list in the build output (S7), swallowed stop/disable errors
(S9) and input size limits (S10).
