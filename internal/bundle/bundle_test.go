package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"bin/amd64/xray":       "xray-amd64",
		"bin/amd64/sneakernet": "sn-amd64",
		"bin/arm64/xray":       "xray-arm64",
		"data/geoip.dat":       "geoip",
		"data/geosite.dat":     "geosite",
		"VERSION":              "v0.1.0\n",
	}
	var sums strings.Builder
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256([]byte(content))
		sums.WriteString(hex.EncodeToString(h[:]) + "  " + rel + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, SumsFile), []byte(sums.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVerify(t *testing.T) {
	dir := makeBundle(t)
	b, err := Open(dir, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Verify(); err != nil {
		t.Fatalf("clean bundle: %v", err)
	}
	if b.Version() != "v0.1.0" {
		t.Errorf("version %q", b.Version())
	}

	// A damaged binary for another architecture does not block this one.
	if err := os.WriteFile(filepath.Join(dir, "bin/arm64/xray"), []byte("flipped"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.Verify(); err != nil {
		t.Fatalf("other-arch damage: %v", err)
	}

	// A damaged shared file does.
	if err := os.WriteFile(filepath.Join(dir, "data/geoip.dat"), []byte("flipped"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "data/geosite.dat"))
	var bad *Corrupt
	if err := b.Verify(); !errors.As(err, &bad) || len(bad.Files) != 2 {
		t.Fatalf("want 2 corrupt files, got %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open(t.TempDir(), "amd64"); err == nil {
		t.Error("empty dir: want error")
	}
	if _, err := Open(makeBundle(t), "386"); err == nil || !strings.Contains(err.Error(), "386") {
		t.Errorf("missing arch: %v", err)
	}
}
