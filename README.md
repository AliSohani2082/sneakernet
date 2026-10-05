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
| Xray-core (VLESS, VMess, Trojan, Shadowsocks, Hysteria2, REALITY) | ✅ M1 |
| v2rayN GUI | 🚧 M2 |
| Sneakernet TUI (terminal UI) | ✅ M1 |
| WireGuard (`wireguard-tools`, imported `.conf` files) | 🗓️ planned |
| OpenVPN | 🗓️ planned |
| ProtonVPN (through its downloadable WireGuard/OpenVPN configs) | 🗓️ planned |
| sing-box | 🗓️ planned |
| Windows support | 🗓️ planned |

## Features

- **Zero network access** during install. Every binary and data file is on the stick.
- **Two install targets:** the running live session, or an installed system on disk set up from the live USB before the first reboot (disk target: planned, M3).
- **Your choice of UI:** terminal UI now, v2rayN GUI planned (M2).
- **Auto-detection** of distro, CPU architecture (amd64, arm64, 386, armv7), libc and init system.
- **Preset servers**, with automatic failover to the fastest working one (passphrase encryption on the stick: planned).
- **systemd service** running as an unprivileged user (system-wide proxy and TUN mode: planned).
- **Integrity checks:** every file is checked against `SHA256SUMS` before anything is installed (signatures: planned).
- **Clean uninstall** from an install manifest.

Supported distros: Debian, Ubuntu, Mint, Fedora, RHEL, Arch, Manjaro, CachyOS and openSUSE.

## Quick start (end user, offline)

1. Boot a Linux ISO from the Sneakernet Ventoy stick (or use an installed system).
2. Open a terminal and run:

```sh
sudo sh /media/$USER/Ventoy/sneakernet/install.sh       # Ubuntu, Debian, Mint
sudo sh /run/media/$USER/Ventoy/sneakernet/install.sh   # Fedora, Arch, openSUSE
```

3. Answer a few questions (server, routing) and you're online:
   SOCKS5 `127.0.0.1:10808`, HTTP `127.0.0.1:10809`.

Always use `sh install.sh`. exFAT has no execute bits, so `./install.sh` fails.

Afterwards (the stick can be unplugged):

| Command | What it does |
|---|---|
| `sudo sneakernet tui` | Terminal UI: pick a server, **test all servers at once**, restart, logs |
| `sneakernet status` | Service state, active server, ports (`--check` tests the connection) |
| `sudo sneakernet switch 12` / `switch auto` | Use another server, or the fastest working one |
| `sudo sneakernet test --all` | Which servers work right now, fastest first |
| `sudo sneakernet doctor` | Diagnose the installation |
| `sudo sneakernet uninstall` | Remove everything |

## Building the stick (maintainer, online machine)

```sh
make fetch            # download the pinned Xray releases (needs internet, once)
cp config/servers.example.txt config/servers.txt && $EDITOR config/servers.txt
make bundle           # → dist/sneakernet/  (~200 MB, amd64 + arm64 + 386 + armv7)
cp -r dist/sneakernet /path/to/Ventoy/
```

The server list takes `vless://`, `vmess://`, `trojan://`, `ss://` and `hysteria2://` links, one per line, or a base64 subscription. Links that the pinned Xray cannot use are listed with the reason and skipped. See [ventoy/README.md](ventoy/README.md) for optional persistence setup.

## Development

```sh
make dev-xray                 # unpack Xray for the tests (after make fetch)
go test ./...                 # unit tests + real-Xray tests, all offline
test/containers/run.sh        # install.sh in offline systemd containers: debian fedora arch
make lint
```

The tests push real traffic through generated configs to a local Xray server,
so they cover link parsing, config generation and the Xray behavior. The
container test runs the real `install.sh` from a read-only, noexec mount,
like the stick, with networking disabled. It checks the service, the service
user, traffic through the proxy, switching servers and uninstall.

## Repository layout

| Path | Purpose |
|---|---|
| `bootstrap/` | `install.sh`, `uninstall.sh`, `README.txt` shipped at the bundle root |
| `cmd/sneakernet/` | The CLI: installer, management commands, TUI entry |
| `internal/` | links, xrayconf, probe, manage, install, service, bundle, detect, target, layout, tui |
| `test/fixtures/` | Dummy servers covering every supported link kind |
| `test/containers/` | Offline systemd container test |
| `templates/desktop/` | v2rayN launcher (M2) |
| `ventoy/` | `ventoy.json` example + integration notes |
| `config/` | Server list; `servers.txt` is gitignored |
| `scripts/` | Fetch pinned deps, assemble the bundle |
| `versions.lock` | Pinned upstream versions + checksums |

## Docs

- [Product requirements (PRD)](docs/PRD.md)
- [Architecture](docs/ARCHITECTURE.md)

## Status

**M1 done:** Xray core, terminal UI, systemd service, install into the running system. Tested offline in Debian 13, Fedora 43 and Arch containers. Next: the v2rayN GUI and system-wide proxy (M2), then install into a system on disk (M3). See the milestones in the PRD.
