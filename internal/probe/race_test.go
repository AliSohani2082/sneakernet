package probe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AliSohani2082/sneakernet/internal/links"
)

// noisyXray is a stand-in "xray" that prints continuously and never opens a
// port, so All gives up during startup while output is still being copied.
func noisyXray(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "xray")
	script := "#!/bin/sh\nwhile :; do echo \"noise from the fake xray\"; done\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func oneServer(t *testing.T) []links.Server {
	t.Helper()
	servers, _, err := links.ParseList(strings.NewReader("trojan://pw@example.com:443?security=tls&sni=example.com#t\n"))
	if err != nil || len(servers) != 1 || !servers[0].Usable() {
		t.Fatalf("fixture: %v %v", servers, err)
	}
	return servers
}

// Run with -race: reading the child's output while the copier still writes
// it used to be a data race on both the timeout and the cancel path.
func TestAllStartupTimeoutReadsOutputSafely(t *testing.T) {
	old := startTimeout
	startTimeout = 400 * time.Millisecond
	defer func() { startTimeout = old }()
	_, err := All(context.Background(), noisyXray(t), t.TempDir(), oneServer(t), "", time.Second)
	if err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("want a startup error, got %v", err)
	}
	if !strings.Contains(err.Error(), "noise from the fake xray") {
		t.Errorf("error should carry the child's last output line: %v", err)
	}
}

func TestAllCancelDuringStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(300 * time.Millisecond); cancel() }()
	_, err := All(ctx, noisyXray(t), t.TempDir(), oneServer(t), "", time.Second)
	if err == nil || !strings.Contains(err.Error(), "did not start") {
		t.Fatalf("want a startup error, got %v", err)
	}
}

func TestTailBufferBounded(t *testing.T) {
	b := &tailBuffer{max: 8}
	b.Write([]byte("0123456789"))
	b.Write([]byte("abc"))
	if got := b.String(); got != "56789abc" {
		t.Errorf("got %q", got)
	}
}
