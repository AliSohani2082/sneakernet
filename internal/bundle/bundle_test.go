package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

func writeSums(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, SumsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sumsWithout(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.HasSuffix(l, "  "+rel) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n") + "\n"
}

func TestIncompleteManifestRejected(t *testing.T) {
	for _, rel := range []string{"bin/amd64/xray", "bin/amd64/sneakernet", "data/geoip.dat", "data/geosite.dat"} {
		dir := makeBundle(t)
		writeSums(t, dir, sumsWithout(t, dir, rel))
		b, err := Open(dir, "amd64")
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Verify(); err == nil || !strings.Contains(err.Error(), rel) {
			t.Errorf("manifest without %s: Verify = %v", rel, err)
		}
		if st, err := b.Stage(t.TempDir()); err == nil {
			st.Cleanup()
			t.Errorf("manifest without %s: Stage must fail", rel)
		}
	}
	// A manifest that lists only one valid file used to pass.
	dir := makeBundle(t)
	b, _ := Open(dir, "amd64")
	one := ""
	for _, l := range strings.Split(sumsWithout(t, dir, "x"), "\n") {
		if strings.HasSuffix(l, "  data/geoip.dat") {
			one = l + "\n"
		}
	}
	writeSums(t, dir, one)
	if err := b.Verify(); err == nil {
		t.Error("single-entry manifest accepted")
	}
}

func TestUnsafeManifestEntriesRejected(t *testing.T) {
	zero := strings.Repeat("0", 64)
	for name, extra := range map[string]string{
		"parent escape":   zero + "  ../outside\n",
		"nested escape":   zero + "  data/../../outside\n",
		"absolute":        zero + "  /etc/passwd\n",
		"unclean":         zero + "  data/./x\n",
		"backslash":       zero + "  data\\x\n",
		"duplicate":       zero + "  data/geoip.dat\n",
		"duplicate (bin)": zero + "  bin/amd64/xray\n",
		"short digest":    "abc  data/x\n",
		"uppercase":       strings.Repeat("A", 64) + "  data/x\n",
	} {
		dir := makeBundle(t)
		old, _ := os.ReadFile(filepath.Join(dir, SumsFile))
		writeSums(t, dir, string(old)+extra)
		b, _ := Open(dir, "amd64")
		if err := b.Verify(); err == nil {
			t.Errorf("%s: Verify accepted the manifest", name)
		}
		if st, err := b.Stage(t.TempDir()); err == nil {
			st.Cleanup()
			t.Errorf("%s: Stage accepted the manifest", name)
		}
	}
}

func TestSymlinkedPayloadRejected(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "evil")
	if err := os.WriteFile(outside, []byte("xray-amd64"), 0o755); err != nil { // same bytes: only the link is wrong
		t.Fatal(err)
	}
	for _, rel := range []string{"bin/amd64/xray", "data/geoip.dat"} {
		dir := makeBundle(t)
		p := filepath.Join(dir, rel)
		os.Remove(p)
		if err := os.Symlink(outside, p); err != nil {
			t.Fatal(err)
		}
		b, _ := Open(dir, "amd64")
		if err := b.Verify(); err == nil {
			t.Errorf("%s: symlink accepted by Verify", rel)
		}
		if st, err := b.Stage(t.TempDir()); err == nil {
			st.Cleanup()
			t.Errorf("%s: symlink accepted by Stage", rel)
		}
	}
	// A symlinked directory component is refused too.
	dir := makeBundle(t)
	os.Rename(filepath.Join(dir, "data"), filepath.Join(dir, "data.real"))
	if err := os.Symlink("data.real", filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	b, _ := Open(dir, "amd64")
	if err := b.Verify(); err == nil {
		t.Error("symlinked data/ accepted")
	}
}

func TestSpecialFileRejected(t *testing.T) {
	dir := makeBundle(t)
	p := filepath.Join(dir, "data/geoip.dat")
	os.Remove(p)
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Skip("no fifo support:", err)
	}
	b, _ := Open(dir, "amd64")
	done := make(chan error, 1)
	go func() { done <- b.Verify() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("FIFO accepted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Verify hung on a FIFO")
	}
}

func TestStageIsolatedFromLaterChangesAndDetectsMutation(t *testing.T) {
	dir := makeBundle(t)
	b, _ := Open(dir, "amd64")
	st, err := b.Stage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Cleanup()
	if fi, _ := os.Stat(st.Dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("staging dir mode %v", fi.Mode().Perm())
	}
	// The stick changing after staging does not reach the staged bytes.
	os.WriteFile(filepath.Join(dir, "bin/amd64/xray"), []byte("swapped after verify"), 0o755)
	if got, _ := os.ReadFile(st.XrayBin()); string(got) != "xray-amd64" {
		t.Errorf("staged xray = %q", got)
	}
	if err := st.Verify(); err != nil {
		t.Errorf("clean staged copy: %v", err)
	}
	// Changing a staged file is detected.
	if err := os.WriteFile(st.XrayBin(), []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	var bad *Corrupt
	if err := st.Verify(); !errors.As(err, &bad) || len(bad.Files) != 1 || bad.Files[0] != "xray" {
		t.Errorf("tampered staged copy: %v", err)
	}
	// Staging a stick whose file no longer matches the manifest fails.
	if st2, err := b.Stage(t.TempDir()); err == nil {
		st2.Cleanup()
		t.Error("Stage accepted a file that differs from SHA256SUMS")
	}
}

func TestOversizedPayloadRejected(t *testing.T) {
	dir := makeBundle(t)
	p := filepath.Join(dir, "data/geoip.dat")
	f, _ := os.OpenFile(p, os.O_WRONLY, 0)
	if err := f.Truncate(maxFileSize + 1); err != nil { // sparse: cheap
		t.Skip(err)
	}
	f.Close()
	b, _ := Open(dir, "amd64")
	if err := b.Verify(); err == nil {
		t.Error("oversized file accepted")
	}
}
