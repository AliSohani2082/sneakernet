package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPastedLinksNeverReachTheLog(t *testing.T) {
	const uuid = "0b1f2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	const pass = "s3cr3t-p4ssw0rd"
	in := "vless://" + uuid + "@example.com:443?security=tls&sni=example.com&type=tcp#paste\n" +
		"trojan://" + pass + "@example.com:443?security=tls&sni=example.com#t2\n" +
		"vless://garbage-" + pass + "\n\n"
	var out, logBuf bytes.Buffer
	u := newUI(strings.NewReader(in), &out)
	u.log = &logBuf
	data, err := pasteLinks(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), uuid) || !strings.Contains(string(data), pass) {
		t.Fatalf("links were not collected: %q", data)
	}
	for _, secret := range []string{uuid, pass} {
		if strings.Contains(logBuf.String(), secret) {
			t.Errorf("log contains %q:\n%s", secret, logBuf.String())
		}
	}
}

func TestOpenLogFileTightensAndRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "install.log")
	if err := os.WriteFile(p, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := openLogFile(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("existing log mode %v, want 0600", fi.Mode().Perm())
	}

	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.log")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openLogFile(link); err == nil {
		f.Close()
		t.Fatal("a symlinked log must be refused")
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Errorf("symlink target was modified: %q", b)
	}
}
