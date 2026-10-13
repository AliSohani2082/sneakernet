# PRD — Sneakernet: offline VPN/proxy kit for Ventoy

| Field | Value |
|---|---|
| Status | v0.2 — M1 implemented (see §12 and §16) |
| Date | 2026-10-04 |
| Owner | TBD |
| Scope | Linux (v1); Windows and other VPN cores later |

---

## 1. Overview

Sneakernet is a self-contained, **fully offline** installer that lives on a Ventoy USB stick next to the Linux ISOs. A user who live-boots any major Linux distro from that USB can run one script that:

1. installs **Xray-core**,
2. optionally installs a **GUI** client (v2rayN) or a **TUI** client (our own `sneakernet tui`),
3. writes a preset list of V2Ray/Xray server outbounds,
4. enables everything as **systemd** services so the machine is online through the proxy right away.

It installs either into the **running live session** or into a **freshly installed Linux system on disk** (from the live session, before the first reboot). The script asks which one.

### 1.1 Problem statement

In countries with heavily filtered internet, a newly installed Linux system has no working route to the open internet. The tools that would fix this (Xray, v2rayN, routing data files) usually have to be downloaded, and those downloads are blocked or unreliable. Users are stuck: they need a proxy to download the proxy.

### 1.2 What "Ventoy plugin" means here

Ventoy is a boot loader. Its "plugins" are entries in `/ventoy/ventoy.json` (persistence, injection, auto-install, …). **Ventoy does not run code inside the booted OS.** So this product is delivered as:

- a **payload folder** (`/sneakernet/`) on the Ventoy exFAT data partition, which every live session can mount, and
- an optional **`ventoy.json` snippet**, mainly the `persistence` plugin so live-session installs survive reboots.

---

## 2. Goals and non-goals

### 2.1 Goals (v1)

- **G1:** Zero network access is needed at any point of installation.
- **G2:** One command installs and starts a working proxy on all major Linux distros (Debian/Ubuntu/Mint, Fedora/RHEL family, Arch family, openSUSE).
- **G3:** The user chooses the UI: **GUI** (v2rayN), **TUI** (`sneakernet tui`), or **headless** (Xray only).
- **G4:** The user chooses the target: **current running system** or **installed system on disk**.
- **G5:** Preset servers ship with the kit, and the proxy works with no manual config.
- **G6:** The kit picks the correct binaries for CPU architecture, libc, distro, and init system automatically.
- **G7:** The architecture can be extended to Windows and to other VPN cores later without a rewrite.

### 2.2 Non-goals (v1)

- Windows and macOS installers (planned for M4).
- Other cores: sing-box, WireGuard, OpenVPN, Hysteria standalone (planned for M5).
- Hooking into distro installers (Calamares, Ubiquity, Anaconda) to run automatically during installation.
- Running a server or relay. This is client-side only.
- Distributing or operating proxy servers. The kit maintainer provides the server list.

---

## 3. Personas

| Persona | Description | Needs |
|---|---|---|
| **End user** | Low-to-medium technical skill. Has the USB and boots a live distro. May be installing Linux for the first time. | Clear prompts, few choices, sensible defaults, works first time, understandable errors. |
| **Kit maintainer** | Technical. Has (some) unrestricted access. Builds the bundle and curates the server list. Hands out USBs or images. | Reproducible builds, pinned versions, easy server-list updates, optional encryption of credentials. |

---

## 4. User stories

- **US-1:** As an end user in a live session, I run `sh install.sh` from the USB, pick "this live session" and "TUI", and within a minute I can browse through the proxy.
- **US-2:** As an end user who just installed Ubuntu from the live USB, I run the installer before rebooting, pick "installed system on disk", and select my new root partition. After reboot, Xray is already running and v2rayN starts on login.
- **US-3:** As an end user, I can switch between preset servers, see connection status, and restart the service from the TUI.
- **US-4:** As an end user, if my distro is too old for the GUI, the installer says so and offers the TUI instead of failing.
- **US-5:** As a kit maintainer, I edit `servers.txt` (share links), run `make bundle`, and copy `dist/sneakernet/` to the USB.
- **US-6:** As a kit maintainer, I encrypt the server list with a passphrase so a lost USB doesn't leak credentials.
- **US-7:** As an end user, I can uninstall everything cleanly with `sh uninstall.sh` or `sneakernet uninstall`.
- **US-8:** As an advanced user, I can run the installer non-interactively: `install.sh --target / --ui tui --yes`.

---

## 5. User flow (interactive)

```
Boot Ventoy → select distro ISO → live desktop
  → open terminal
  → sudo sh /run/media/<user>/Ventoy/sneakernet/install.sh
      1. Verify bundle integrity (SHA256SUMS + signature)
      2. Detect: arch, distro, glibc, init system, desktop environment
      3. Ask target:
           [1] This running system (live session / current OS)
           [2] An installed system on disk  → list Linux root partitions → mount at /mnt/target
      4. Ask UI:
           [1] TUI (recommended for all systems)
           [2] GUI (v2rayN)     ← disabled with reason if requirements unmet
           [3] None (Xray headless)
      5. Ask routing preset: Global / Bypass LAN / Bypass LAN + domestic (region rules)
      6. Ask: set system-wide proxy? [Y/n]   Enable TUN mode? [y/N]
      7. (If servers.age) ask passphrase
      8. Install files → generate config → enable + start services
      9. Run connectivity self-test (local SOCKS port answers; optional outbound probe)
     10. Print summary: ports, how to open the TUI/GUI, how to uninstall
```

---

## 6. Functional requirements

### 6.1 Bootstrap and integrity

| ID | Requirement | Priority |
|---|---|---|
| FR-1 | `install.sh` is POSIX `sh` (no bash-isms) and runs from an exFAT mount where files are not executable. | P0 |
| FR-2 | The bootstrap maps `uname -m` → `amd64` / `arm64` / `386` / `armv7`, copies the matching `sneakernet` binary to a temporary directory, makes it executable, and runs it. | P0 |
| FR-3 | The bootstrap re-runs itself with `sudo`/`pkexec` when not root. | P0 |
| FR-4 | `sneakernet` verifies every bundle file this CPU needs against `SHA256SUMS` before installing (damaged files for other architectures do not block). A `minisign` signature over `SHA256SUMS` is planned (M3). | P0 (signature: P1) |

### 6.2 Detection

| ID | Requirement | Priority |
|---|---|---|
| FR-5 | Detect distro and family from `/etc/os-release` (`ID`, `ID_LIKE`, `VERSION_ID`). | P0 |
| FR-6 | Detect the init system (systemd / OpenRC / runit) on the **target**, not only the host. | P0 |
| FR-7 | Detect glibc version and musl. Detect whether the desktop environment is GNOME, KDE, XFCE, other, or none. | P0 |
| FR-8 | Detect a live session (overlay/squashfs root, `boot=casper`, `rd.live.image`, `archiso`, etc.) to pick the default target and warn about non-persistence. | P1 |

### 6.3 Target selection

| ID | Requirement | Priority |
|---|---|---|
| FR-9 | Target **running system**: install under `/`. | P0 |
| FR-10 | Target **installed system**: list candidate Linux root partitions (`lsblk -f`, ext4/btrfs/xfs, containing `/etc/os-release`). Handle btrfs subvolumes (`@`) and LUKS (prompt to unlock). Mount at `/mnt/target`. | P0 (LUKS: P1) |
| FR-11 | For a disk target, all paths are written relative to the target root. Services are enabled offline with `systemctl --root=/mnt/target enable …`. The installer never runs target binaries on the host, apart from optional chroot checks. | P0 |
| FR-12 | Install into the **target user's** home for per-user items (GUI autostart, v2rayN config). The user is detected from `/home/*` or the UID ≥ 1000 entries in the target's `/etc/passwd`, with a prompt if ambiguous. | P0 |

### 6.4 Components

| ID | Requirement | Priority |
|---|---|---|
| FR-13 | **Xray-core:** install to `/opt/sneakernet/bin/xray`, with data files in `/opt/sneakernet/share/` (`geoip.dat`, `geosite.dat`) and config in `/etc/sneakernet/config.json` (mode 0640, owner `root:sneakernet`). Our own paths and unit name never clash with an existing Xray install (`/usr/local/bin/xray`, `xray.service`). | P0 |
| FR-14 | **TUI:** install `sneakernet` to `/opt/sneakernet/bin/` with a `/usr/local/bin/sneakernet` link. `sneakernet tui` is a live search menu over the servers: results update while typing, name matches rank before property matches (protocol, transport, security, host, SNI, port, path), `field:value` qualifiers narrow results. Actions: use the selected server, test the speed of all results, use the fastest result, auto mode, add servers (paste with live preview), remove servers, sort by relevance/speed, logs, restart, start/stop. With a missing or empty list it opens on the add screen. | P0 |
| FR-15 | **GUI (v2rayN):** install the self-contained Linux build to `/opt/v2rayN`, with a `.desktop` launcher and XDG autostart for the target user. Pre-seed v2rayN's config with the server list and point it at the bundled Xray core. | P1 |
| FR-16 | **GUI preflight:** check glibc, required shared libraries (`ldd` on the v2rayN binary against the target), and the presence of a graphical session. If a check fails, explain why and offer the TUI. | P1 |
| FR-17 | **Mode exclusivity:** in GUI mode, `sneakernet-xray.service` is installed but **disabled** by default, because v2rayN manages its own core. In TUI/headless mode, `sneakernet-xray.service` is enabled and started. | P0 |

### 6.5 Configuration

| ID | Requirement | Priority |
|---|---|---|
| FR-18 | Parse share links in `servers.txt`: `vless://`, `vmess://`, `trojan://`, `ss://`, `hysteria2://`, including transports (tcp/ws/grpc/xhttp/httpupgrade) and security (tls/reality). Unknown entries are skipped with a warning. | P0 |
| FR-19 | The server list lives at `sneakernet/servers.txt` on the stick, editable by the user (excluded from `SHA256SUMS`, CRLF and BOM tolerated). Missing or empty is allowed: the installer offers to paste links, and servers can be added later (`sneakernet tui`, `sneakernet add`). A re-install merges the stick list into the installed one. | P0 |
| FR-20 | Generate an Xray config with inbounds (SOCKS 10808 and HTTP 10809 on 127.0.0.1), all outbounds tagged, and a selected default. An optional `balancer` + `observatory` gives automatic failover. | P0 |
| FR-21 | Routing presets: `global`, `bypass-lan`, `bypass-lan-region` (region list configurable; it uses bundled `.dat` files). | P0 |
| FR-22 | Validate the generated config with `xray run -test` before enabling the service (host-arch only; skipped with a warning for a foreign-arch disk target). | P0 |
| FR-23 | Encrypted server list `servers.age`: decrypt with the passphrase prompted at install (age scrypt recipient). The plaintext is never written to the USB. | P1 |

### 6.6 Services and system integration

| ID | Requirement | Priority |
|---|---|---|
| FR-24 | Install `sneakernet-xray.service` (systemd) running as the `sneakernet` system user (created offline with `systemd-sysusers`, also with `--root` for disk targets), with `Restart=on-failure`, `CAP_NET_BIND_SERVICE` only, and systemd sandboxing (`ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, …). `CAP_NET_ADMIN` is added only when TUN is enabled. | P0 |
| FR-25 | Optional system-wide proxy settings: `/etc/environment.d/90-sneakernet.conf` (http/https/all/no_proxy); GNOME via `gsettings`; KDE via `kwriteconfig5`/`6`. For a disk target, apply on first login through a one-shot autostart. | P1 |
| FR-26 | Optional TUN mode through Xray's native TUN inbound (requires a recent core), with routing so that the whole system goes through the proxy. | P2 |
| FR-27 | OpenRC/runit fallback service scripts. On an unsupported init system, install the files, print manual-start instructions, and don't fail. | P2 |

### 6.7 Operations

| ID | Requirement | Priority |
|---|---|---|
| FR-28 | `sneakernet doctor`: checks binaries, config validity, service state, listening ports, DNS, and an outbound probe; prints actionable fixes. | P1 |
| FR-29 | `sneakernet uninstall` / `uninstall.sh`: removes every file listed in the install manifest (`/var/lib/sneakernet/manifest.json`), disables services, and reverts proxy settings. | P0 |
| FR-30 | Re-running the installer is idempotent: it upgrades files in place and preserves the user's chosen server unless `--reset` is given. | P0 |
| FR-31 | Non-interactive flags: `--target <path>`, `--ui tui\|gui\|none`, `--routing <preset>`, `--sysproxy`, `--tun`, `--user <name>`, `--yes`, `--dry-run`. | P1 |
| FR-32 | All actions are logged to `/var/log/sneakernet-install.log` on the target, plus a copy in the bundle directory when it's writable (for support). | P1 |

---

## 7. Non-functional requirements

| ID | Requirement |
|---|---|
| NFR-1 | **Offline:** the installer makes no network requests. CI enforces this by running the install in a network-less VM. |
| NFR-2 | **Portability:** all binaries we ship are static (`CGO_ENABLED=0`). v2rayN uses its upstream self-contained build. No package manager is used, so no `apt`, `dnf`, or `pacman` dependencies are needed. |
| NFR-3 | **Size:** the full bundle (all architectures plus the GUI) is ≤ 300 MB. A "lite" bundle (amd64, no GUI) is ≤ 60 MB. |
| NFR-4 | **Speed:** an interactive install takes under 60 s on USB 2.0, excluding user think-time. |
| NFR-5 | **Safety:** the installer never formats or repartitions anything. Disk targets are only mounted read-write after the user confirms, and are unmounted on exit, including on error. |
| NFR-6 | **Usability:** at most 5 questions in the default path, every question has a default, and the text is plain English. Translation into the local language (e.g. Persian) is P2. |
| NFR-7 | **Maintainability:** upstream versions are pinned in `versions.lock`, and updating them is a one-line change plus `make bundle`. |

---

## 8. Compatibility matrix (v1 target)

| Distro family | Versions | amd64 | arm64 | TUI/headless | GUI (v2rayN) |
|---|---|---|---|---|---|
| Ubuntu / Mint / Pop!_OS | 22.04+ | ✅ | ✅ | ✅ | ⚠️ newer releases only* |
| Debian | 12+ | ✅ | ✅ | ✅ | ⚠️ 13+* |
| Fedora | 40+ | ✅ | ✅ | ✅ | ⚠️ 43+* |
| RHEL / Alma / Rocky | 9+ | ✅ | ✅ | ✅ | ⚠️ 10+* |
| Arch / Manjaro / EndeavourOS / CachyOS | rolling | ✅ | ✅ | ✅ | ✅ |
| openSUSE Tumbleweed / Leap | TW, 15.6+ | ✅ | ✅ | ✅ | ⚠️ |
| Alpine / Void / Artix (non-systemd) | — | ✅ | ✅ | ⚠️ P2 fallback | ❌ |
| 32-bit x86 / armv7 | — | ✅ | — | ✅ headless/TUI | ❌ |

\* Recent v2rayN Linux releases list minimum distro versions. Mitigation: pin a v2rayN version that still supports older bases, and ship a second "legacy" GUI build if needed. The GUI preflight check (FR-16) decides at install time.

---

## 9. Security and privacy

- **Credentials on removable media:** the server list contains secrets (UUIDs, passwords, Reality keys). We support encryption with `servers.age`, and `README` tells maintainers to use it.
- **Supply chain:** pinned upstream versions with upstream SHA256 checksums verified at build time. The bundle is signed with `minisign`, and the public key is embedded in `sneakernet`.
- **Least privilege:** Xray runs as the `sneakernet` system user, with systemd hardening (`NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`). The config is not world-readable.
- **No telemetry.** The connectivity self-test only probes a user-configurable URL.
- **User risk:** using circumvention tools can be legally risky in some jurisdictions. The README must say so plainly. The product does not hide its own presence. It is a convenience installer, not a stealth tool.

---

## 10. Ventoy integration

- **Payload location:** `/<Ventoy data partition>/sneakernet/`. The default exFAT works, and NTFS is also supported. All live kernels ≥ 5.7 mount exFAT natively.
- **exFAT has no Unix permissions:** run as `sh install.sh`. Binaries are copied out before they are executed (FR-1, FR-2).
- **Persistence (optional):** `ventoy/ventoy.json.example` shows a `persistence` entry so that a live-session install survives reboot, with a backend `.dat` image created by Ventoy's `CreatePersistentImg.sh`.
- **Merging config:** `ventoy/README.md` explains how to merge the snippet into an existing `ventoy.json` without breaking other plugins.
- **Injection plugin:** not required. It extracts files into the initramfs, which doesn't reliably reach the final live root filesystem across distros. It may be revisited for a desktop shortcut in the live session (P2).

---

## 11. Build and release process (maintainer)

1. Edit `versions.lock` when updating Xray (pinned: v26.9.9; checksums are the GitHub release digests).
2. `make fetch` (needs internet): downloads the pinned Xray zips into `.cache/deps` and verifies them. `scripts/fetch-deps.sh urls` prints the links for downloading by hand.
3. `make build`: cross-compiles `sneakernet` for linux/{amd64, arm64, 386, armv7} (static, `CGO_ENABLED=0`).
4. Add servers: write `config/servers.txt` (gitignored). Encryption (`servers.age`) is planned (FR-23).
5. `make bundle` (offline): assembles `dist/sneakernet/` and writes `SHA256SUMS`. ~200 MB for all four architectures.
6. Copy `dist/sneakernet/` to the root of the Ventoy partition (and optionally merge `ventoy.json`).
7. Offline distribution of updates: hand over a new folder, zip, or USB. No online update channel is planned for v1.

---

## 12. Milestones

| Milestone | Scope | Exit criteria |
|---|---|---|
| **M0** ✅ | Repo scaffold, PRD, architecture doc | This document approved |
| **M1** ✅ | Bootstrap, detection, link parser, config generation, Xray + systemd, TUI, uninstall, running-system target | Offline install passes in systemd containers (Debian 13, Fedora 43, Arch) with `--network=none`; live-ISO VM runs pending (needs VT-x) |
| **M2** | v2rayN GUI + preflight, system proxy (GNOME/KDE), `doctor` | GUI works on Ubuntu 26.04, Fedora 43, Arch; graceful fallback on older systems |
| **M3** | Disk target (incl. btrfs, LUKS), encrypted server list, TUN mode, OpenRC fallback | US-2 passes on 3 distros; LUKS + btrfs case covered |
| **M4** | Windows: PowerShell bootstrap, `sneakernet.exe`, v2rayN-windows, Windows service via `sc`/WinSW | Offline install on Windows 10/11 |
| **M5** | Core plugin interface (sing-box, WireGuard, OpenVPN) | A second core implemented behind the same interface |

---

## 13. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| v2rayN raises its minimum distro versions | GUI unusable on older distros | Pin the version; legacy GUI build; preflight + TUI fallback |
| Version skew between Xray and v2rayN | GUI fails to start the core | `versions.lock` pins compatible pairs; tested together in CI |
| Preset servers get blocked (DPI changes) | Kit installs but cannot connect | Multiple servers + balancer failover; easy server update; `servers.d/` for local additions |
| Live session without persistence | Install lost on reboot | Warn clearly; recommend a disk target or Ventoy persistence |
| Distro-specific quirks (SELinux, AppArmor, immutable distros like Silverblue) | Service fails to start | SELinux context restore (`restorecon`) on Fedora/RHEL; detect immutable systems and install to `/usr/local` (writable) or warn |
| exFAT automount paths differ by distro | User can't find the script | README covers the common paths; bootstrap prints its own location; `find / -name install.sh -path '*sneakernet*'` hint |
| Legal exposure for users | Harm to users | Clear disclaimer; no stealth features |

---

## 14. Success metrics

- ≥ 95 % successful offline installs across the v1 compatibility matrix in CI VMs.
- Median time from running the script to a working proxy is under 2 minutes, including prompts.
- Zero network calls during install (verified in CI).
- An end user can complete US-1 without documentation (usability test with 5 users).

---

## 15. Open questions

1. Which v2rayN version should be pinned for the broadest distro support, and do we ship two GUI builds?
2. Region routing data: which `.dat` source(s) to bundle by default, and how often to refresh?
3. Should the TUI also handle importing new share links typed or pasted by the user? Proposed: yes, P1.
4. Should Persian (and other) translations of installer prompts be in M1 or later?
5. Signing key custody: who holds the minisign private key for official bundles?

---

## 16. Implementation notes (M1)

Facts about the pinned Xray (v26.9.9) that shaped the code, verified against its source (`infra/conf/*.go`) and the binary:

- **Config keys are not validated.** Xray ignores unknown JSON keys, so `xray run -test` does not catch a misspelled field. Golden tests in `internal/xrayconf` pin the field names.
- **Plaintext VLESS/Trojan to a public address is refused.** Links with no TLS/REALITY and no VLESS encryption are listed as unusable instead of failing at start.
- **`allowInsecure` was removed.** Links that ask for it are kept with a warning; certificate pinning uses `pcs` (`pinnedPeerCertSha256`) and `vcn`.
- **REALITY** needs a fingerprint (default `chrome`), a 32-byte public key (`password`, formerly `publicKey`), a hex short id of at most 16 characters, and raw/xhttp/grpc transport. A post-quantum key (`pqv`) must decode to 1952 bytes; truncated ones are dropped with a warning.
- **Removed transports:** `h2`/`http`/`quic` are reported as unusable. `tcp` is now called `raw`; both are accepted.
- **Servers refuse private targets by default** (`freedom` blackholes loopback/private IPs behind a proxy inbound). This only matters for the local test server; the client's own `direct` outbound is unaffected, so LAN bypass works.
- **Service account:** the first design used `DynamicUser` + `LoadCredential`. It works on real systems but not in rootless containers, LXC or similar, where the credentials mount fails. M1 uses a static `sneakernet` user from `systemd-sysusers`, which works everywhere systemd does and with `--root`.

- **Read-only systemd and sysusers directories (NixOS):** `/etc/systemd/system` and `/etc/sysusers.d` can be links into the read-only Nix store. On the running system the unit then goes to `/run/systemd/system` (enabled with `--runtime`) and the user entry to `/run/sysusers.d`. Both last until reboot, and the installer says so. Note that on a read-only filesystem even deleting a missing file fails with EROFS, so cleanup checks for existence first.
- **The `sneakernet` command on PATH:** linked in `/usr/local/bin` when that is on PATH; otherwise in the first writable, non-package-managed PATH directory. On NixOS that is `/run/wrappers/bin`, first on every PATH there and cleared on reboot or `nixos-rebuild`. If nothing fits, the installer prints the full path to use.

Test layers that exist now:

| Layer | What | Command |
|---|---|---|
| Unit | link parsing, config shape, detection, bundle checks, service commands | `go test ./...` |
| Xray | every link kind passes `xray run -test`; real traffic through a local Xray server (VLESS raw/ws/REALITY+Vision, Trojan, Shadowsocks) | `make dev-xray && go test ./...` |
| CLI | the real installer driven with typed answers into a temp root, `systemctl --root`, `systemd-analyze verify` | `go test ./cmd/...` |
| System | `install.sh` from a read-only noexec mount in offline systemd containers; service, user, traffic, switch, uninstall | `test/containers/run.sh` |
| Live ISO | Ventoy image in QEMU | planned (needs VT-x enabled) |

