#!/usr/bin/env bash
# Offline install test in real systemd containers.
#
#   test/containers/run.sh                 # debian fedora arch
#   test/containers/run.sh debian          # just one
#
# For each distro: boot systemd with networking disabled (--network=none),
# mount the bundle read-only and noexec (like the exFAT stick), run the real
# install.sh, and check that the service runs as a dynamic user and carries
# traffic to a local Xray server inside the container. Needs podman, make
# dev-xray output, and the distro images (pulled once, online).
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
WORK="$ROOT/.cache/container-test"
UUID=a8d31bbb-0d00-4762-b870-8c23e19d0a8c
DISTROS=("${@:-debian fedora arch}")
read -r -a DISTROS <<<"${DISTROS[*]}"

declare -A IMAGE=(
  [debian]=docker.io/library/debian:13
  [ubuntu]=docker.io/library/ubuntu:26.04
  [fedora]=docker.io/library/fedora:43
  [arch]=docker.io/library/archlinux:latest
)

say()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
pass() { printf '  \033[32mPASS\033[0m %s\n' "$*"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$*"; FAILED=$((FAILED + 1)); }
FAILED=0

# --- build the test bundle (amd64 only) with servers inside the container ---
say "building test bundle"
rm -rf "$WORK" && mkdir -p "$WORK"
cat > "$WORK/servers.txt" <<EOF
vless://$UUID@127.0.0.1:41001?type=tcp#container vless
trojan://sneakernet-test@127.0.0.1:41002?security=none#container trojan
vless://$UUID@127.0.0.1:41009?type=tcp#nobody listens here
EOF
make -C "$ROOT" -s build ARCHES=amd64 VERSION=container-test >/dev/null || exit 1
SERVERS="$WORK/servers.txt" ARCHES=amd64 VERSION=container-test \
  sh "$ROOT/scripts/make-bundle.sh" "$WORK/stick/sneakernet" >/dev/null || exit 1
CGO_ENABLED=0 go build -o "$WORK/echo204" "$ROOT/test/containers/echo204" || exit 1
cat > "$WORK/server.json" <<EOF
{"log":{"loglevel":"warning"},
 "inbounds":[
  {"listen":"127.0.0.1","port":41001,"protocol":"vless","settings":{"clients":[{"id":"$UUID"}],"decryption":"none"}},
  {"listen":"127.0.0.1","port":41002,"protocol":"trojan","settings":{"clients":[{"password":"sneakernet-test"}]}}],
 "outbounds":[{"protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.0/8"]}]}}]}
EOF

run_distro() {
  local d=$1 img=${IMAGE[$1]:-} name="sneakernet-test-$1" tag="localhost/sneakernet-test:$1"
  [ -n "$img" ] || { fail "$d: unknown distro"; return; }
  say "$d ($img)"
  if ! podman image exists "$tag"; then
    podman build -q -t "$tag" --build-arg BASE="$img" -f "$ROOT/test/containers/Containerfile" \
      "$ROOT/test/containers" >/dev/null || { fail "$d: image build"; return; }
  fi
  podman rm -f "$name" >/dev/null 2>&1
  # --privileged (still rootless) lets systemd set up the mount namespaces that
  # DynamicUser and the sandboxing options need, as on a real booted system.
  podman run -d --name "$name" --network=none --systemd=always --privileged \
    -v "$WORK/stick:/media/Ventoy:ro,noexec" \
    -v "$WORK/echo204:/usr/local/libexec/echo204:ro" \
    -v "$WORK/server.json:/etc/test-server.json:ro" \
    "$tag" >/dev/null || { fail "$d: container start"; return; }
  x() { podman exec "$name" "$@"; }

  for _ in $(seq 50); do
    st=$(x systemctl is-system-running 2>/dev/null)
    [[ $st == running || $st == degraded ]] && break
    sleep 0.2
  done

  # The "internet" and the proxy server, both inside the offline container.
  x systemd-run -q --unit=echo204 /usr/local/libexec/echo204
  x systemd-run -q --unit=test-server -E XRAY_LOCATION_ASSET=/media/Ventoy/sneakernet/data \
    /bin/sh -c 'cp /media/Ventoy/sneakernet/bin/amd64/xray /run/xray-server && exec /run/xray-server run -c /etc/test-server.json'
  sleep 1

  local out
  if out=$(x sh /media/Ventoy/sneakernet/install.sh --yes --server 1 --routing bypass-lan 2>&1); then
    pass "install.sh from a read-only noexec stick"
  else
    fail "install.sh"; echo "$out" | sed 's/^/      /'; podman rm -f "$name" >/dev/null; return
  fi
  [[ $(x systemctl is-active sneakernet-xray) == active ]] && pass "service active" || {
    fail "service active"; x journalctl -u sneakernet-xray --no-pager -n 20 | sed 's/^/      /'; }
  [[ $(x systemctl is-enabled sneakernet-xray) == enabled ]] && pass "service enabled at boot" || fail "service enabled"
  local pid user
  pid=$(x systemctl show -p MainPID --value sneakernet-xray)
  user=$(x ps -o user= -p "$pid" 2>/dev/null | tr -d ' ')
  [[ $user == sneakernet ]] && pass "xray runs as the sneakernet user" || fail "xray runs as '$user'"
  [[ $(x stat -c '%a %U:%G' /etc/sneakernet/config.json) == "640 root:sneakernet" ]] &&
    pass "config is 0640 root:sneakernet" || fail "config owner/mode: $(x stat -c '%a %U:%G' /etc/sneakernet/config.json)"

  out=$(x sneakernet test --url http://127.0.0.1:41080/generate_204 2>&1)
  [[ $out == *ok* ]] && pass "traffic flows through the proxy (${out#*: })" || { fail "proxy traffic"; echo "      $out"; }

  out=$(x sneakernet switch 2 2>&1)
  out2=$(x sneakernet test --url http://127.0.0.1:41080/generate_204 2>&1)
  [[ $out == *"now using #2"* && $out2 == *ok* ]] && pass "switch to trojan server" || {
    fail "switch"; echo "      $out / $out2"; }

  out=$(x sneakernet test --all --url http://127.0.0.1:41080/generate_204 2>&1)
  [[ $out == *"2 of 3 servers work"* ]] && pass "test --all finds 2 of 3 working" || {
    fail "test --all"; echo "$out" | sed 's/^/      /'; }

  x systemctl restart sneakernet-xray
  sleep 0.5
  out=$(x sneakernet status --check 2>&1)
  [[ $out == *"active (running)"* ]] && pass "status after restart" || { fail "status"; echo "$out" | sed 's/^/      /'; }

  out=$(x sneakernet uninstall --yes 2>&1)
  if [[ $out == *removed* ]] && ! x test -e /opt/sneakernet && ! x test -e /etc/systemd/system/sneakernet-xray.service \
     && ! x systemctl is-active -q sneakernet-xray && ! x id sneakernet >/dev/null 2>&1; then
    pass "uninstall removes everything"
  else
    fail "uninstall"; echo "$out" | sed 's/^/      /'
  fi
  podman rm -f "$name" >/dev/null
}

for d in "${DISTROS[@]}"; do run_distro "$d"; done
echo
if ((FAILED)); then echo "$FAILED check(s) failed"; exit 1; fi
echo "all container checks passed"
