package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfinedRootRejectsEscapingSymlinks(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	// <root>/etc -> <outside>: an absolute symlink out of the root.
	if err := os.Symlink(outside, filepath.Join(root, "etc")); err != nil {
		t.Fatal(err)
	}
	x, err := OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	if err := x.WriteFile("/etc/sneakernet/state.json", []byte("x"), 0o600); err == nil {
		t.Fatal("write through an escaping symlink succeeded")
	}
	if err := x.MkdirAll("/etc/sneakernet", 0o755); err == nil {
		t.Error("mkdir through an escaping symlink succeeded")
	}
	if err := x.RemoveAll("/etc/sneakernet"); err == nil {
		t.Error("remove through an escaping symlink succeeded")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("host directory was written to: %v", ents)
	}
}

func TestConfinedRootRejectsDotDotAndRelativeEscape(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink("../"+filepath.Base(outside), filepath.Join(root, "up")); err != nil {
		t.Fatal(err)
	}
	x, _ := OpenRoot(root)
	defer x.Close()
	if err := x.WriteFile("/up/f", []byte("x"), 0o600); err == nil {
		t.Error("relative symlink escape accepted")
	}
	// A plain ".." in the path is clamped to the root, never the host.
	if err := x.WriteFile("/../../escape", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); err != nil {
		t.Errorf("expected the file inside the root: %v", err)
	}
}

func TestWriteReplacesFinalSymlinkInsteadOfFollowingIt(t *testing.T) {
	for name, open := range map[string]func(string) (*Root, error){
		"confined": OpenRoot,
		"host":     func(string) (*Root, error) { return OpenRoot("/") },
	} {
		root := t.TempDir()
		victim := filepath.Join(t.TempDir(), "victim")
		os.WriteFile(victim, []byte("precious"), 0o644)
		unit := filepath.Join(root, "unit")
		if err := os.Symlink(victim, unit); err != nil {
			t.Fatal(err)
		}
		x, err := open(root)
		if err != nil {
			t.Fatal(err)
		}
		path := "/unit"
		if name == "host" {
			path = unit
		}
		if err := x.WriteFile(path, []byte("new"), 0o644); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		x.Close()
		if b, _ := os.ReadFile(victim); string(b) != "precious" {
			t.Errorf("%s: symlink target was overwritten: %q", name, b)
		}
		if fi, _ := os.Lstat(unit); fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s: symlink still in place", name)
		}
		if b, _ := os.ReadFile(unit); string(b) != "new" {
			t.Errorf("%s: unit = %q", name, b)
		}
	}
}

func TestCheckDir(t *testing.T) {
	root := t.TempDir()
	x, _ := OpenRoot(root)
	defer x.Close()

	os.Mkdir(filepath.Join(root, "ok"), 0o755)
	if err := x.CheckDir("/ok"); err != nil {
		t.Errorf("plain directory: %v", err)
	}
	for _, mode := range []os.FileMode{0o775, 0o757, 0o777} {
		d := filepath.Join(root, "open")
		os.Mkdir(d, 0o700)
		os.Chmod(d, mode)
		if err := x.EnsureDir("/open", 0o755); err == nil || !strings.Contains(err.Error(), "writable") {
			t.Errorf("mode %o accepted: %v", mode, err)
		}
		os.Remove(d)
	}
	os.Mkdir(filepath.Join(root, "real"), 0o755)
	os.Symlink("real", filepath.Join(root, "link"))
	if err := x.EnsureDir("/link", 0o755); err == nil {
		t.Error("symlink to a directory accepted as a managed directory")
	}
	os.WriteFile(filepath.Join(root, "file"), nil, 0o644)
	if err := x.EnsureDir("/file", 0o755); err == nil {
		t.Error("regular file accepted as a directory")
	}
	if err := x.EnsureDir("/new/deeper", 0o750); err != nil {
		t.Errorf("creating a missing directory: %v", err)
	}
}

func TestRemoveAllRefusesRoot(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "keep"), nil, 0o644)
	x, _ := OpenRoot(root)
	defer x.Close()
	if err := x.RemoveAll("/"); err == nil {
		t.Error("RemoveAll(/) must be refused")
	}
	if _, err := os.Stat(filepath.Join(root, "keep")); err != nil {
		t.Error(err)
	}
}
