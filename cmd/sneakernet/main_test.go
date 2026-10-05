package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/xraytest"
)

// makeBundle builds a bundle dir around the real Xray build, with the
// fixture servers as the stick's servers.txt.
func makeBundle(t *testing.T) string {
	t.Helper()
	dir := makeBundleWithout(t)
	data, err := os.ReadFile("../../test/fixtures/servers.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "servers.txt"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// makeBundleWithout builds a bundle with no servers.txt.
func makeBundleWithout(t *testing.T) string {
	t.Helper()
	xray, assets := xraytest.Binary(t)
	dir := t.TempDir()
	src := map[string]string{
		"bin/amd64/xray":   xray,
		"data/geoip.dat":   filepath.Join(assets, "geoip.dat"),
		"data/geosite.dat": filepath.Join(assets, "geosite.dat"),
	}
	var sums strings.Builder
	add := func(rel string, data []byte) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, 0o755); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(data)
		sums.WriteString(hex.EncodeToString(h[:]) + "  " + rel + "\n")
	}
	for rel, s := range src {
		data, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		add(rel, data)
	}
	add("bin/amd64/sneakernet", []byte("#!/bin/sh\n"))
	os.WriteFile(filepath.Join(dir, "VERSION"), []byte("v0.1.0-test\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(sums.String()), 0o644)
	return dir
}

// systemdRoot is an empty root filesystem that looks like a systemd system,
// as an installed distro mounted from the live session would.
func systemdRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "usr/lib/systemd/systemd")
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func sn(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(stdin), &out)
	return out.String(), code
}

// TestCLIInstallFlow drives the real installer with typed answers, as a user
// on a serial console would, into a temp root.
func TestCLIInstallFlow(t *testing.T) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("no systemctl")
	}
	b := makeBundle(t)
	root := systemdRoot(t)

	// (--root skips the target question) interface 2 (disabled → warned) then 1 ·
	// server: list, bad number, 3 · routing 2 (bypass-region) · country code "IR"
	answers := "2\n1\nl\n99\n3\n2\nIR\n"
	out, code := sn(t, answers, "install", "--bundle", b, "--root", root)
	if code != 0 {
		t.Fatalf("install exit %d:\n%s", code, out)
	}
	for _, want := range []string{
		"bundle v0.1.0-test is intact",
		"13 servers in the list, 12 usable",
		"1 skipped: no TLS or encryption",
		"Graphical app (v2rayN) is not available",
		"there is no server #99",
		"vless ws tls",
		"will start at boot",
		"Routing        bypass-region (ir)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("install output lacks %q\n%s", want, out)
		}
	}
	var st map[string]any
	raw, _ := os.ReadFile(filepath.Join(root, layout.StateFile))
	json.Unmarshal(raw, &st)
	if st["index"] != 3.0 || st["routing"] != "bypass-region" || st["region"] != "ir" {
		t.Errorf("state: %v", st)
	}

	out, code = sn(t, "", "status", "--root", root)
	if code != 0 || !strings.Contains(out, `#3 "vless ws tls"`) {
		t.Errorf("status (%d):\n%s", code, out)
	}
	out, code = sn(t, "", "list", "--root", root)
	if code != 0 || !strings.Contains(out, "*    3  vless ws tls") || !strings.Contains(out, "plaintext vless") {
		t.Errorf("list (%d):\n%s", code, out)
	}
	out, code = sn(t, "", "switch", "--root", root, "auto")
	if code != 0 || !strings.Contains(out, "now using auto") {
		t.Errorf("switch (%d):\n%s", code, out)
	}
	out, code = sn(t, "", "switch", "--root", root, "13")
	if code == 0 || !strings.Contains(out, "cannot be used") {
		t.Errorf("switch to unusable server should fail (%d):\n%s", code, out)
	}
	out, code = sn(t, "", "doctor", "--root", root)
	if code != 0 || !strings.Contains(out, "Xray accepts the config") {
		t.Errorf("doctor (%d):\n%s", code, out)
	}

	// Re-running install offers to keep the settings.
	out, code = sn(t, "1\ny\n", "install", "--bundle", b, "--root", root)
	if code != 0 || !strings.Contains(out, "already installed (server auto") {
		t.Errorf("re-install (%d):\n%s", code, out)
	}

	out, code = sn(t, "n\n", "uninstall", "--root", root)
	if code != 0 {
		t.Errorf("uninstall declined (%d):\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, layout.StateFile)); err != nil {
		t.Fatal("declined uninstall removed files")
	}
	out, code = sn(t, "", "uninstall", "--root", root, "--yes")
	if code != 0 || !strings.Contains(out, "Sneakernet is removed") {
		t.Errorf("uninstall (%d):\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, layout.OptDir)); !os.IsNotExist(err) {
		t.Error("uninstall left files behind")
	}
}

func TestCLINonInteractive(t *testing.T) {
	b := makeBundle(t)
	root := t.TempDir()
	out, code := sn(t, "", "install", "--bundle", b, "--root", root, "--yes", "--server", "2",
		"--routing", "global", "--socks-port", "20808", "--http-port", "20809")
	if code != 0 || !strings.Contains(out, "127.0.0.1:20808") {
		t.Fatalf("install --yes (%d):\n%s", code, out)
	}
	cfg, _ := os.ReadFile(filepath.Join(root, layout.ConfigFile))
	if !bytes.Contains(cfg, []byte(`"port": 20808`)) || !bytes.Contains(cfg, []byte("xhttp.example.com")) {
		t.Errorf("config:\n%s", cfg)
	}
}

func TestCLIInputClosed(t *testing.T) {
	out, code := sn(t, "1\n", "install", "--bundle", makeBundle(t), "--root", t.TempDir())
	if code == 0 || !strings.Contains(out, "input closed") {
		t.Errorf("EOF mid-install should fail cleanly (%d):\n%s", code, out)
	}
}

func TestCLICorruptBundle(t *testing.T) {
	b := makeBundle(t)
	os.WriteFile(filepath.Join(b, "data/geoip.dat"), []byte("bit rot"), 0o644)
	out, code := sn(t, "", "install", "--bundle", b, "--root", t.TempDir(), "--yes")
	if code == 0 || !strings.Contains(out, "data/geoip.dat") {
		t.Errorf("corrupt bundle (%d):\n%s", code, out)
	}
}

func TestConvert(t *testing.T) {
	out, code := sn(t, "", "convert", "--servers", "../../test/fixtures/servers.txt", "--server", "1")
	if code != 0 {
		t.Fatalf("convert (%d): %s", code, out)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("convert output is not JSON: %v\n%s", err, out)
	}
}

const (
	linkA = "vless://a8d31bbb-0d00-4762-b870-8c23e19d0a8c@a.example.com:443?type=ws&security=tls#pasted A"
	linkB = "trojan://pw@b.example.com:443#pasted B"
)

func TestCLIEmptyStickListPaste(t *testing.T) {
	b := makeBundleWithout(t)
	root := systemdRoot(t)
	// interface 1 · paste: a junk line, two links, empty line · server 2 · routing 1
	answers := "1\nnot a link\n" + linkA + "\n" + linkB + "\n\n2\n1\n"
	out, code := sn(t, answers, "install", "--bundle", b, "--root", root)
	if code != 0 {
		t.Fatalf("install (%d):\n%s", code, out)
	}
	for _, want := range []string{"no usable servers", "Paste share links now", "not a share link",
		"pasted A", "pasted B", `#2 "pasted B"`, "will start at boot"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
	list, _ := os.ReadFile(filepath.Join(root, layout.ServersFile))
	if string(list) != linkA+"\n"+linkB+"\n" {
		t.Errorf("installed list:\n%s", list)
	}
}

func TestCLINoServersThenAddRemove(t *testing.T) {
	b := makeBundleWithout(t)
	root := systemdRoot(t)
	out, code := sn(t, "", "install", "--bundle", b, "--root", root, "--yes")
	if code != 0 || !strings.Contains(out, "proxy is off until you add servers") {
		t.Fatalf("install --yes without servers (%d):\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(root, layout.ConfigFile)); !os.IsNotExist(err) {
		t.Error("config written without servers")
	}
	wants := filepath.Join(root, "etc/systemd/system/multi-user.target.wants", layout.UnitName)
	if _, err := os.Lstat(wants); !os.IsNotExist(err) {
		t.Error("service enabled without servers")
	}

	out, code = sn(t, linkA+"\n"+linkB+"\n"+linkA+"\n", "add", "--root", root)
	if code != 0 || !strings.Contains(out, "added 2, 1 already in the list") || !strings.Contains(out, "switch auto") {
		t.Fatalf("add (%d):\n%s", code, out)
	}
	out, code = sn(t, "", "switch", "--root", root, "auto")
	if code != 0 {
		t.Fatalf("switch (%d):\n%s", code, out)
	}
	if _, err := os.Lstat(wants); err != nil {
		t.Errorf("switch should enable the service: %v", err)
	}

	// Re-installing from the stick keeps servers added after install.
	out, code = sn(t, "", "install", "--bundle", makeBundle(t), "--root", root, "--yes", "--server", "auto")
	if code != 0 || !strings.Contains(out, "15 servers in the list") {
		t.Fatalf("re-install (%d):\n%s", code, out)
	}

	out, code = sn(t, "", "remove", "--root", root, "1")
	if code != 0 || !strings.Contains(out, `removed #1 "pasted A"`) {
		t.Fatalf("remove (%d):\n%s", code, out)
	}
	list, _ := os.ReadFile(filepath.Join(root, layout.ServersFile))
	if strings.Contains(string(list), linkA) || !strings.Contains(string(list), linkB) {
		t.Errorf("list after remove:\n%s", list)
	}
}
