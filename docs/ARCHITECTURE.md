# Architecture — v2ray-kit

## Components

```
Ventoy USB (exFAT)                          Target system (/ or /mnt/target)
└── v2ray-kit/                              ├── /usr/local/bin/xray
    ├── install.sh ──(1) arch detect        ├── /usr/local/bin/v2kit        (TUI + CLI)
    │        │                              ├── /usr/local/share/xray/*.dat
    │        └─(2) cp bin/<arch>/v2kit →    ├── /usr/local/etc/xray/config.json
    │              $TMP, chmod, exec        ├── /etc/systemd/system/xray.service
    ├── bin/<arch>/{v2kit,xray}             ├── /opt/v2rayN/                (GUI, optional)
    ├── gui/<arch>/v2rayN-linux-*.zip       ├── ~user/.config/autostart/v2rayN.desktop
    ├── data/*.dat                          ├── /etc/environment.d/90-v2kit.conf (optional)
    ├── config/servers.txt|servers.age      └── /var/lib/v2kit/manifest.json (for uninstall)
    └── SHA256SUMS(.minisig)
```

### Why a thin shell bootstrap + Go binary
- exFAT has no exec bits, so the shell script only has to work with `sh install.sh`.
- Parsing share links (base64, URL encoding, nested JSON for vmess) and producing JSON is fragile in shell. Go makes it testable.
- A single static binary has no runtime dependencies on any distro.
- The same code cross-compiles to Windows for M4.

## `v2kit` packages (`internal/`)

| Package | Responsibility |
|---|---|
| `detect` | Arch, `/etc/os-release` (of the *target* root), init system, glibc/musl, desktop environment, live-session detection |
| `target` | Abstracts the target root: `Running{}` (`/`) vs `Disk{root}` (mount, btrfs subvolumes, LUKS unlock, cleanup on exit) |
| `links` | Parses `vless/vmess/trojan/ss/hysteria2` URIs into typed `Server` structs |
| `xrayconf` | Builds Xray JSON: inbounds, outbounds, routing presets, balancer/observatory; validates with `xray run -test` |
| `install` | Copies files into the target, records each path in the manifest, idempotent upgrades |
| `service` | `Manager` interface with a systemd implementation (offline `--root` enable) and OpenRC/runit stubs |
| `sysproxy` | env file, GNOME gsettings, KDE kwriteconfig; first-login one-shot for disk targets |
| `bundle` | SHA256SUMS + minisign verification; locates bundle assets by arch |
| `tui` | Bubble Tea app: server list, switch outbound (rewrite config + restart), status, log tail |

### Extension points
- **Cores (M5):** `core.Core` interface (`Install`, `RenderConfig(servers)`, `ServiceUnit`, `Validate`). Xray is the first implementation; sing-box and WireGuard come later.
- **OS (M4):** `target`, `service`, and `sysproxy` get Windows implementations behind build tags (`_windows.go`), and `install.ps1` replaces `install.sh`.

## Install sequence

1. `install.sh`: escalate to root → detect arch → copy `v2kit` to `$TMPDIR` → `exec v2kit install "$@" --bundle <dir>`.
2. `bundle.Verify` → `detect` (host) → prompt for target → `target.Open` → `detect` (target).
3. Prompt for UI, routing, sysproxy/TUN → load servers (decrypt if `.age`) → `xrayconf.Build` → validate.
4. `install` the files → `service.Enable` (start it only when the target is the running system) → `sysproxy.Apply`.
5. Write the manifest → self-test → summary → `target.Close` (unmount).
