// Package xraytest runs a throwaway Xray server on localhost so tests can
// push real traffic through generated client configs without internet.
package xraytest

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Binary finds the Xray build used by tests: $SNEAKERNET_XRAY, or the copy
// `make dev-xray` unpacks into <repo>/.cache/xray-amd64. It skips the test
// when neither exists. The asset dir (geo data) is the binary's directory.
func Binary(t testing.TB) (bin, assets string) {
	t.Helper()
	bin = os.Getenv("SNEAKERNET_XRAY")
	if bin == "" {
		if root, err := repoRoot(); err == nil {
			bin = filepath.Join(root, ".cache/xray-amd64/xray")
		}
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no xray binary; run `make dev-xray` or set SNEAKERNET_XRAY")
	}
	return bin, filepath.Dir(bin)
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// Credentials shared by the server and the links it hands out.
const (
	UUID     = "a8d31bbb-0d00-4762-b870-8c23e19d0a8c"
	Password = "sneakernet-test"
	// REALITY key pair from `xray x25519`.
	RealityPrivate = "oLSc82l4hrHRiOyNA70PxZ72ud-n4KcZnew97IG8fmE"
	RealityPublic  = "3D8x05l98bV_wuYKxnyW_SAodVkQkOz6LWhCkiT07DY"
	RealityShortID = "6ba85179e30d4fc2"
	RealitySNI     = "reality.test"
)

// Server is a running local Xray server.
type Server struct {
	// Links are share links (name in the fragment) for each inbound.
	Links []string
	// DeadLink points at a port nothing listens on.
	DeadLink string
	// ProbeURL answers 204, served locally.
	ProbeURL string
}

// Start launches an Xray server with VLESS (raw, ws, REALITY), Trojan and
// Shadowsocks inbounds on 127.0.0.1, plus a local HTTP endpoint that answers
// 204. Everything is stopped when the test ends.
func Start(t testing.TB, bin, assets string) *Server {
	t.Helper()
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(probe.Close)

	// REALITY borrows the TLS handshake of a real TLS 1.3 site; a local one will do.
	decoy := httptest.NewUnstartedServer(http.NotFoundHandler())
	decoy.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	decoy.Config.ErrorLog = log.New(io.Discard, "", 0) // REALITY probes it with odd handshakes
	decoy.StartTLS()
	t.Cleanup(decoy.Close)

	ports := freePorts(t, 6)
	vlessUser := []map[string]any{{"id": UUID}}
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{
			{"listen": "127.0.0.1", "port": ports[0], "protocol": "vless",
				"settings": map[string]any{"clients": vlessUser, "decryption": "none"}},
			{"listen": "127.0.0.1", "port": ports[1], "protocol": "vless",
				"settings":       map[string]any{"clients": vlessUser, "decryption": "none"},
				"streamSettings": map[string]any{"network": "ws", "wsSettings": map[string]any{"path": "/ws"}}},
			{"listen": "127.0.0.1", "port": ports[2], "protocol": "vless",
				"settings": map[string]any{"clients": []map[string]any{{"id": UUID, "flow": "xtls-rprx-vision"}},
					"decryption": "none"},
				"streamSettings": map[string]any{"network": "raw", "security": "reality",
					"realitySettings": map[string]any{
						"target":      strings.TrimPrefix(decoy.URL, "https://"),
						"serverNames": []string{RealitySNI},
						"privateKey":  RealityPrivate,
						"shortIds":    []string{RealityShortID},
					}}},
			{"listen": "127.0.0.1", "port": ports[3], "protocol": "trojan",
				"settings": map[string]any{"clients": []map[string]any{{"password": Password}}}},
			{"listen": "127.0.0.1", "port": ports[4], "protocol": "shadowsocks",
				"settings": map[string]any{"method": "chacha20-ietf-poly1305", "password": Password, "network": "tcp,udp"}},
		},
		// Xray servers refuse private targets by default; the probe endpoint is on localhost.
		"outbounds": []map[string]any{{"protocol": "freedom", "settings": map[string]any{
			"finalRules": []map[string]any{{"action": "allow", "ip": []string{"127.0.0.0/8"}}},
		}}},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "server.json")
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assets)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	for _, p := range ports[:5] {
		if !waitPort(p, 10*time.Second) {
			t.Fatalf("xray server did not listen on %d:\n%s", p, out.String())
		}
	}

	ss := "chacha20-ietf-poly1305:" + Password
	return &Server{
		Links: []string{
			fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp#local vless raw", UUID, ports[0]),
			fmt.Sprintf("vless://%s@127.0.0.1:%d?type=ws&path=%%2Fws#local vless ws", UUID, ports[1]),
			fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=reality&sni=%s&fp=chrome&pbk=%s&sid=%s&flow=xtls-rprx-vision#local vless reality",
				UUID, ports[2], RealitySNI, RealityPublic, RealityShortID),
			fmt.Sprintf("trojan://%s@127.0.0.1:%d?security=none#local trojan", Password, ports[3]),
			fmt.Sprintf("ss://%s@127.0.0.1:%d#local ss", base64.RawURLEncoding.EncodeToString([]byte(ss)), ports[4]),
		},
		DeadLink: fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp#dead", UUID, ports[5]),
		ProbeURL: probe.URL + "/generate_204",
	}
}

func freePorts(t testing.TB, n int) []int {
	t.Helper()
	var ls []net.Listener
	var ports []int
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ls = append(ls, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	for _, l := range ls {
		l.Close()
	}
	return ports
}

func waitPort(port int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
			c.Close()
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
