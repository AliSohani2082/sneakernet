# 🔌 Sneakernet

**Carry the connection in your pocket.**

Sneakernet is a self-contained, **fully offline** installer for VPN and proxy clients. It lives next to your ISOs on a [Ventoy](https://www.ventoy.net) USB stick. Boot any Linux live ISO, run one script, and you're connected, with nothing to download.

You pick a protocol and a GUI or terminal UI. Sneakernet then:
- installs the right binaries for your distro and CPU,
- sets up your preset servers,
- starts everything as a systemd service.

It works in the live session or on the system you just installed.

> ⚠️ Using circumvention tools may be legally risky where you live. Use at your own risk.

## Why

On a heavily filtered network, a fresh Linux install can't reach the open internet. The tools that would fix that have to be downloaded, and those downloads are blocked. Sneakernet breaks that loop by carrying everything on the stick.

## Supported clients

| Protocol / client | Status |
|---|---|
| Xray-core (VLESS, VMess, Trojan, Shadowsocks, Hysteria2, REALITY) | 🚧 v1 |
| v2rayN GUI | 🚧 v1 |
| Sneakernet TUI (terminal UI) | 🚧 v1 |
| WireGuard (`wireguard-tools`, imported `.conf` files) | 🗓️ planned |
| OpenVPN | 🗓️ planned |
| ProtonVPN (through its downloadable WireGuard/OpenVPN configs) | 🗓️ planned |
| sing-box | 🗓️ planned |
| Windows support | 🗓️ planned |

## Features

- **Zero network access** during install. Every binary and data file is on the stick.
- **Two install targets:** the running live session, or an installed system on disk (set up from the live USB before the first reboot).
- **Your choice of UI:** GUI, TUI, or headless.
- **Auto-detection** of distro, CPU architecture (amd64, arm64, 386, armv7), libc and init system.
- **Preset servers**, optionally encrypted on the stick with a passphrase.
- **systemd services**, with optional system-wide proxy and TUN mode.
- **Integrity checks:** checksums and a signature are verified before anything is installed.
- **Clean uninstall** from an install manifest.

Supported distros: Debian, Ubuntu, Mint, Fedora, RHEL, Arch, Manjaro, CachyOS and openSUSE.

## Quick start (end user, offline)

1. Boot a Linux ISO from the Sneakernet Ventoy stick.
2. Open a terminal and run:

```sh
sudo sh /media/$USER/Ventoy/sneakernet/install.sh       # Ubuntu, Debian, Mint
sudo sh /run/media/$USER/Ventoy/sneakernet/install.sh   # Fedora, Arch, openSUSE
```

3. Answer a few questions (target, UI, routing) and you're online.

Always use `sh install.sh`. exFAT has no execute bits, so `./install.sh` fails.

## Building the stick (maintainer, online machine)

```sh
make fetch            # download pinned upstream binaries (needs internet)
cp config/servers.example.txt config/servers.txt && $EDITOR config/servers.txt
make bundle           # → dist/sneakernet/
cp -r dist/sneakernet /path/to/Ventoy/
```

See [ventoy/README.md](ventoy/README.md) for optional persistence setup.

## Repository layout

| Path | Purpose |
|---|---|
| `bootstrap/` | POSIX `install.sh` / `uninstall.sh` shipped at the bundle root |
| `cmd/v2kit/` | Go entry point (install, uninstall, tui, doctor, convert) |
| `internal/` | detect, target, links, xrayconf, install, service, sysproxy, bundle, tui |
| `templates/` | systemd unit, .desktop file, client config templates |
| `ventoy/` | `ventoy.json` example + integration notes |
| `config/` | server list; `servers.txt` / `servers.age` are gitignored |
| `scripts/` | fetch pinned deps, assemble the bundle, VM tests |
| `versions.lock` | pinned upstream versions + checksums |

## Docs

- [Product requirements (PRD)](docs/PRD.md)
- [Architecture](docs/ARCHITECTURE.md)

## Status

Early scaffold (milestone M0). Nothing is installable yet. See the milestones in the PRD.
