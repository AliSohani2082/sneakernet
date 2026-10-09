# 🔌 Sneakernet

**Carry the connection in your pocket.**

Sneakernet is a self-contained, **fully offline** installer for VPN and proxy clients. It lives next to your ISOs on a [Ventoy](https://www.ventoy.net) USB stick. Boot any Linux live ISO, run one script, and you're connected, with nothing to download.

You pick a protocol and a GUI or terminal UI. Sneakernet then:
- installs the right binaries for your distro and CPU,
- sets up your preset servers,
- starts everything as a systemd service.

It works in the live session or on the system you just installed.

> ⚠️ Using circumvention tools may be legally risky where you live. Use at your own risk.

## Quick start

Online machine: put Sneakernet on your Ventoy stick (plug it in first):

```sh
# from source (works today; needs Go 1.26+, git, curl, unzip, make)
git clone https://github.com/AliSohani2082/sneakernet && cd sneakernet && make fetch stick

# or from a published release (once one exists, see "One-liners" below)
curl -fsSL https://raw.githubusercontent.com/AliSohani2082/sneakernet/main/scripts/install.sh | sh
```

Offline machine: boot a live ISO from the stick, open a terminal and run (one command, nothing is downloaded):

```sh
sudo sh "$(ls -d /run/media/*/Ventoy /media/*/Ventoy /mnt/ventoy 2>/dev/null | head -n1)/sneakernet/install.sh"
```

If that finds nothing, the stick is not mounted yet: see [Using the stick](#using-the-stick-end-user-offline) or `README.txt` on the stick. Then use the proxy at SOCKS5 `127.0.0.1:10808` / HTTP `127.0.0.1:10809`, and `sudo sneakernet tui` to manage servers.

## Why

On a heavily filtered network, a fresh Linux install can't reach the open internet. The tools that would fix that have to be downloaded, and those downloads are blocked. Sneakernet breaks that loop by carrying everything on the stick.

## Supported clients

| Protocol / client | Status |
|---|---|
| Xray-core (VLESS, VMess, Trojan, Shadowsocks, Hysteria2, REALITY) | ✅ M1 |
| v2rayN GUI | 🚧 M2 |
| Sneakernet TUI: live search, speed test, add/remove | ✅ M1 |
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
- **Integrity checks:** every file is checked against `SHA256SUMS` before anything is installed, and only the checked copies are installed or run. This catches a damaged or half-copied stick, **not a malicious one**: the checksums sit on the same stick. See [Security notes](docs/SECURITY.md); signatures are planned.
- **Clean uninstall** from an install manifest.

Supported distros: Debian, Ubuntu, Mint, Fedora, RHEL, Arch, Manjaro, CachyOS and openSUSE.

## Using the stick (end user, offline)

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
| `sudo sneakernet tui` | Search menu over your servers: type to filter, test speed, use the fastest, add/remove (see below) |
| `sneakernet status` | Service state, active server, ports (`--check` tests the connection) |
| `sudo sneakernet switch 12` / `switch auto` | Use another server, or the fastest working one |
| `sudo sneakernet test --all` | Which servers work right now, fastest first |
| `sudo sneakernet add links.txt` / `remove 12` | Add servers (from files or stdin) or remove one |
| `sudo sneakernet doctor` | Diagnose the installation |
| `sudo sneakernet uninstall` | Remove everything |

### The search menu (`sudo sneakernet tui`)

Start typing and the list filters live. Matches in the server **name** come first, then matches in other properties (protocol, transport, security, host, SNI, port, path…). Matched letters are highlighted, and small typos still match (`grmny` finds "Germany"). `field:value` narrows to one property: `sec:reality`, `proto:vless`, `port:443`, `host:example.com`, `sni:apple`.

| Key | Action |
|---|---|
| type / `esc` | search / clear the search (`esc` again quits) |
| `↑` `↓` `pgup` `pgdn` | move |
| `enter` | use the selected server |
| `^t` | test the speed of every server in the results (one Xray process, all in parallel) |
| `^b` | use the fastest server in the results (tests them first if needed) |
| `^a` | auto mode: Xray keeps measuring all servers and uses the fastest |
| `^n` | add servers: paste links, see a live preview, `^s` to save |
| `^x` | remove the selected server (asks first) |
| `^o` | sort by relevance or speed |
| `^l` `^r` `^s` | logs · restart · start/stop |

With no servers yet, the TUI opens straight into the add screen. Pasting links into the search box opens it too. "Speed" is the time to fetch a small page through each server, the same "real delay" v2rayN shows.

## One-liners

| Goal | Command |
|---|---|
| Build the stick from source | `git clone https://github.com/AliSohani2082/sneakernet && cd sneakernet && make fetch stick` |
| Release onto the stick (online) | `curl -fsSL https://raw.githubusercontent.com/AliSohani2082/sneakernet/main/scripts/install.sh \| sh` |
| Same, a pinned release + checksum | `curl -fsSLO https://raw.githubusercontent.com/AliSohani2082/sneakernet/main/scripts/install.sh && sh install.sh --version v1.0.0 --sha256 <sha256 from the release page>` |
| Bundle you already have onto the stick (offline) | `sh scripts/install.sh --from dist/sneakernet` or `--from sneakernet.tar.gz` |
| Install from the stick (live ISO, offline) | `sudo sh /run/media/$USER/Ventoy/sneakernet/install.sh` |
| Just the CLI/TUI, with Go | `go install github.com/AliSohani2082/sneakernet/cmd/sneakernet@latest` |
| Just the CLI/TUI, with Nix | `nix run github:AliSohani2082/sneakernet` or `nix profile install github:AliSohani2082/sneakernet` |

`scripts/install.sh` copies the bundle onto the stick as `<stick>/sneakernet/`. It finds a mounted stick named `Ventoy` (or takes `--to /path`), checks the archive against its `.sha256` (or your `--sha256`) and every file against `SHA256SUMS`, both before and after copying, and keeps a `servers.txt` that is already on the stick. It never uses sudo. With `--from` it does not touch the network. `sh scripts/install.sh -h` lists the options.

> **Release downloads:** no release has been published yet, so the `curl … | sh` lines fail with "download failed" for now. A release must carry `sneakernet.tar.gz` and `sneakernet.tar.gz.sha256`, which `make tarball` builds. To use another mirror, set `SNEAKERNET_URL` (default `https://github.com/AliSohani2082/sneakernet/releases`). Piping into `sh` runs whatever the server sends; on a network you don't trust, download `install.sh`, read it, and pass `--sha256`.

`go install` and `nix` give you only the `sneakernet` binary, without Xray or the stick bundle. That is enough for `sneakernet convert`, and for managing an existing install, but `sneakernet install` needs a bundle (`--bundle <stick>/sneakernet`). Use `make bundle` / the release tarball to build a stick.

## Building the stick (maintainer, online machine)

```sh
make fetch            # download the pinned Xray releases (needs internet, once)
cp config/servers.example.txt config/servers.txt && $EDITOR config/servers.txt
make bundle           # → dist/sneakernet/  (~200 MB, amd64 + arm64 + 386 + armv7)
make stick            # bundle + copy to the mounted stick, verified (VENTOY=/path/to/Ventoy to choose)
make tarball          # → dist/sneakernet.tar.gz + .sha256 (the release asset)
```

`make help` lists all targets. `ARCHES="amd64"` builds a smaller bundle for one CPU. `cp -r dist/sneakernet /path/to/Ventoy/` works too; `make stick` also verifies the copy and keeps the stick's `servers.txt`.

The server list ends up as `sneakernet/servers.txt` on the stick. It can be edited there directly, even from Windows; it is not covered by the checksums. It takes `vless://`, `vmess://`, `trojan://`, `ss://` and `hysteria2://` links, one per line, or a base64 subscription. Links that the pinned Xray cannot use are listed with the reason and skipped. If the list is missing or empty, the installer asks you to paste links, or to continue and add them later in the TUI. See [ventoy/README.md](ventoy/README.md) for optional persistence setup.

## Nix

The flake provides the `sneakernet` CLI/TUI (Linux), an app, and a dev shell with everything the Makefile needs (Go, make, curl, unzip, shellcheck…):

```sh
nix run github:AliSohani2082/sneakernet -- version     # run without installing
nix profile install github:AliSohani2082/sneakernet    # install the CLI
nix develop                                            # dev shell, then: make fetch bundle
nix build && ./result/bin/sneakernet version           # build from a checkout
nix flake check                                        # build + cmd/sneakernet tests
```

In a NixOS configuration, add the input and put `inputs.sneakernet.packages.${pkgs.stdenv.hostPlatform.system}.default` in `environment.systemPackages`. There is no NixOS module: on NixOS, the stick's installer is the wrong tool (it writes to `/opt` and `/etc`); use the `services.xray` module with the config from `sneakernet convert`. After changing `go.mod`, update `vendorHash` in `flake.nix`: set it to `lib.fakeHash`, run `nix build`, copy the hash from the error.

## Development

```sh
make dev-xray                 # unpack Xray for the tests (after make fetch)
go test ./...                 # unit tests + real-Xray tests, all offline
test/containers/run.sh        # install.sh in offline systemd containers: debian fedora arch
make lint                     # gofmt, go vet, sh -n, shellcheck (if installed)
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
| `internal/` | links, search, xrayconf, probe, manage, install, service, bundle, detect, target, layout, tui |
| `test/fixtures/` | Dummy servers covering every supported link kind |
| `test/containers/` | Offline systemd container test |
| `templates/desktop/` | v2rayN launcher (M2) |
| `ventoy/` | `ventoy.json` example + integration notes |
| `config/` | Server list; `servers.txt` is gitignored |
| `scripts/` | Fetch pinned deps, assemble the bundle, copy it onto a stick (`install.sh`) |
| `flake.nix` | Nix package, app and dev shell |
| `versions.lock` | Pinned upstream versions + checksums |

## Docs

- [Product requirements (PRD)](docs/PRD.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Security notes](docs/SECURITY.md)

## Status

**M1 done:** Xray core, terminal UI, systemd service, install into the running system. Tested offline in Debian 13, Fedora 43 and Arch containers. Next: the v2rayN GUI and system-wide proxy (M2), then install into a system on disk (M3). See the milestones in the PRD.
