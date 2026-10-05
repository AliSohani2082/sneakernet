# Architecture — Sneakernet

## Big picture

```
Ventoy USB (exFAT, no exec bits)              Target system
└── sneakernet/                               ├── /opt/sneakernet/bin/{xray,sneakernet}
    ├── install.sh ─(1) uname -m → arch       ├── /opt/sneakernet/share/{geoip,geosite}.dat
    │     └─(2) copy bin/<arch>/sneakernet    ├── /usr/local/bin/sneakernet → /opt/…/sneakernet
    │           to an exec-able temp dir,     ├── /etc/sneakernet/
    │           run `sneakernet install`      │     config.json   0640 root:sneakernet (generated)
    ├── uninstall.sh                          │     servers.txt   0600 (copied from the stick)
    ├── bin/<arch>/{sneakernet,xray}          │     state.json    0600 (chosen server, routing, ports)
    ├── data/{geoip,geosite}.dat              ├── /etc/sysusers.d/sneakernet.conf → user "sneakernet"
    ├── config/servers.txt                    ├── /etc/systemd/system/sneakernet-xray.service
    ├── SHA256SUMS  VERSION  README.txt       ├── /var/lib/sneakernet/manifest.json
    └── licenses/                             └── /var/log/sneakernet-install.log
```

Nothing is downloaded at install time. Everything sneakernet creates has its
own name (`/opt/sneakernet`, `sneakernet-xray.service`), so an existing Xray
install is never touched.

## Why a thin shell bootstrap + one Go binary

- exFAT has no execute permission, so `install.sh` must work as `sh install.sh`.
  It only picks the binary for the CPU and runs it from a temp dir. It tries
  several dirs, because `/tmp` is `noexec` on some systems.
- Parsing share links (base64, URL encoding, vmess JSON) and writing Xray
  JSON is fragile in shell and easy to test in Go.
- One static binary (`CGO_ENABLED=0`) runs on every distro and libc. It is the
  installer, the manager (`status`, `switch`, `test`, `doctor`, `uninstall`)
  and the TUI. The same code cross-compiles to Windows later (M4).

## Packages

| Package | Responsibility |
|---|---|
| `cmd/sneakernet` | CLI: interactive installer and management commands. `main` is a thin wrapper around `run()` so tests can drive the real CLI with typed answers |
| `internal/links` | Parse `vless/vmess/trojan/ss/hysteria2` links into `Server`. Flags what the pinned Xray rejects (`Problem`) and what was dropped (`Warnings`) |
| `internal/xrayconf` | Build the Xray config: SOCKS+HTTP inbounds, one server or a `leastPing` balancer + observatory ("auto"), routing presets. `Validate` runs `xray run -test`. `BuildProbe` makes one config with a SOCKS port per server |
| `internal/probe` | Fetch a 204 URL through a SOCKS proxy; test every server at once with one temporary Xray process |
| `internal/manage` | The installed state: read the server list and `state.json`, `Apply` (build → validate → write config), `Switch` (+ restart) |
| `internal/install` | Copy the bundle into the target, create the service user, enable the service, write the manifest; uninstall |
| `internal/service` | systemd: install/enable/restart units, offline (`systemctl --root`) for non-running targets; the `sneakernet` user via `systemd-sysusers` |
| `internal/bundle` | Locate the payload for this CPU and verify `SHA256SUMS` (files for other CPUs are skipped) |
| `internal/detect` | os-release family, init system, libc, live session, architecture |
| `internal/target` | The root to install into: the running system (`/`) or a mounted directory |
| `internal/layout` | Every installed path, in one place |
| `internal/tui` | Bubble Tea UI over `manage` and `probe` |
| `internal/fsutil` | Atomic file writes (temp file + rename) |
| `internal/xraytest` | Test helper: a local Xray server (VLESS raw/ws/REALITY, Trojan, Shadowsocks) and a 204 endpoint |

## Install sequence

1. `install.sh`: escalate to root → map `uname -m` → copy `bin/<arch>/sneakernet` to an executable temp dir → `sneakernet install --bundle <stick dir>`.
2. Verify `SHA256SUMS` → ask for the target (M1: running system only) → detect distro, init system, live session.
3. Ask for the interface (M1: TUI), the server (`auto` or a number), and the routing preset (+ country code). A previous install's settings are offered as defaults.
4. Copy binaries, geo data and the server list → create the `sneakernet` user (`systemd-sysusers`) → link `/usr/local/bin/sneakernet`.
5. `manage.Apply`: build the config, check it with the installed Xray (`run -test`), write config 0640 and state.
6. Install and enable `sneakernet-xray.service` (start it if the target is running) → write the manifest.
7. Wait for the SOCKS port and fetch a 204 URL through it → print ports, proxy exports and next commands.

Re-running the installer replaces files in place and keeps the settings if
the user agrees. `uninstall` reads the manifest. It removes the unit, the
user, the command link and only sneakernet's own directories.

## Extension points

- **Disk target (M3):** `target.Dir(root)` already works end to end in the tests: files, `systemd-sysusers --root`, `systemctl --root enable`. Missing pieces: choosing and mounting the partition (btrfs subvolumes, LUKS), and skipping `xray run -test` when the target CPU differs (`manage.Manager.SkipValidate`).
- **Other cores (M5):** `xrayconf` + `probe` + the unit file are the Xray-specific parts. A `core` interface (`Parse`, `BuildConfig`, `Validate`, `Unit`) would let WireGuard or sing-box slot in beside it.
- **Windows (M4):** `service`, `target` and `detect` get `_windows.go` versions; `install.ps1` replaces `install.sh`.

## Testing

See the table in [PRD §16](PRD.md#16-implementation-notes-m1). In short:
`make dev-xray && go test ./...` covers everything except real systemd. Real
systemd is covered by `test/containers/run.sh`, which uses offline podman
containers for Debian, Fedora and Arch.
