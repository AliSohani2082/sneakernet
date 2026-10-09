// Package fsutil has small file helpers that keep half-written files from
// ever being visible (everything is written to a temp file and renamed) and
// that keep privileged writes inside a chosen root.
//
// A Root opened on "/" is the running host: paths are used as given, and the
// host's own symlinks (NixOS, usr-merge) work as usual. A Root opened on any
// other directory is confined with os.Root: no path component may resolve
// outside it, so a symlink such as <root>/etc -> /etc is an error rather than
// a write to the host.
package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Root performs file operations on paths given as absolute paths inside the
// root ("/etc/sneakernet/state.json").
type Root struct {
	r *os.Root // nil: the host filesystem, unconfined
}

// OpenRoot opens dir as a root. "" and "/" mean the host filesystem.
func OpenRoot(dir string) (*Root, error) {
	if dir == "" || filepath.Clean(dir) == "/" {
		return &Root{}, nil
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Root{r: r}, nil
}

// Close releases the root.
func (x *Root) Close() error {
	if x.r == nil {
		return nil
	}
	return x.r.Close()
}

func (x *Root) rel(p string) string {
	rel := strings.TrimPrefix(filepath.Clean("/"+p), "/")
	if rel == "" {
		return "."
	}
	return rel
}

func (x *Root) openFile(p string, flag int, perm os.FileMode) (*os.File, error) {
	if x.r == nil {
		return os.OpenFile(p, flag, perm)
	}
	return x.r.OpenFile(x.rel(p), flag, perm)
}

func (x *Root) rename(from, to string) error {
	if x.r == nil {
		return os.Rename(from, to)
	}
	return x.r.Rename(x.rel(from), x.rel(to))
}

// Lstat is os.Lstat inside the root.
func (x *Root) Lstat(p string) (fs.FileInfo, error) {
	if x.r == nil {
		return os.Lstat(p)
	}
	return x.r.Lstat(x.rel(p))
}

// Readlink is os.Readlink inside the root.
func (x *Root) Readlink(p string) (string, error) {
	if x.r == nil {
		return os.Readlink(p)
	}
	return x.r.Readlink(x.rel(p))
}

// ReadFile reads a file inside the root.
func (x *Root) ReadFile(p string) ([]byte, error) {
	f, err := x.openFile(p, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// MkdirAll is os.MkdirAll inside the root.
func (x *Root) MkdirAll(p string, perm os.FileMode) error {
	if x.r == nil {
		return os.MkdirAll(p, perm)
	}
	return x.r.MkdirAll(x.rel(p), perm)
}

// Chmod is os.Chmod inside the root.
func (x *Root) Chmod(p string, perm os.FileMode) error {
	if x.r == nil {
		return os.Chmod(p, perm)
	}
	return x.r.Chmod(x.rel(p), perm)
}

// Chown is os.Chown inside the root.
func (x *Root) Chown(p string, uid, gid int) error {
	if x.r == nil {
		return os.Chown(p, uid, gid)
	}
	return x.r.Chown(x.rel(p), uid, gid)
}

// Remove is os.Remove inside the root. A final symlink is removed, not followed.
func (x *Root) Remove(p string) error {
	if x.r == nil {
		return os.Remove(p)
	}
	return x.r.Remove(x.rel(p))
}

// RemoveAll is os.RemoveAll inside the root.
func (x *Root) RemoveAll(p string) error {
	if x.r == nil {
		return os.RemoveAll(p)
	}
	if x.rel(p) == "." {
		return errors.New("refusing to remove the root itself")
	}
	return x.r.RemoveAll(x.rel(p))
}

// Symlink creates link -> target inside the root. The target text is stored
// as is; it is not resolved.
func (x *Root) Symlink(target, link string) error {
	if x.r == nil {
		return os.Symlink(target, link)
	}
	return x.r.Symlink(target, x.rel(link))
}

// EnsureDir creates dir (and parents) if needed, then checks it with CheckDir.
func (x *Root) EnsureDir(dir string, perm os.FileMode) error {
	if _, err := x.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := x.MkdirAll(dir, perm); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return x.CheckDir(dir)
}

// CheckDir fails unless dir is a real directory (not a symlink), owned by
// root (or by the current user, so unprivileged tests work) and not writable
// by group or others. Privileged code must not trust a directory someone
// else can add files to.
func (x *Root) CheckDir(dir string) error {
	fi, err := x.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s exists but is not a plain directory (symlink or file); remove it and run again", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		if st.Uid != 0 && int(st.Uid) != os.Geteuid() {
			return fmt.Errorf("%s is owned by user %d, not root; refusing to install into it", dir, st.Uid)
		}
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by group or others (mode %04o); refusing to install into it", dir, fi.Mode().Perm())
	}
	return nil
}

// WriteFile atomically replaces path with data. An existing file or symlink
// at path is replaced, never written through.
func (x *Root) WriteFile(path string, data []byte, perm os.FileMode) error {
	return x.replace(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// CopyFile atomically copies src (a host path) to dst (a path in the root).
// Replacing a running executable this way is safe: the old inode stays alive
// until the process exits.
func (x *Root) CopyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return x.replace(dst, perm, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
}

func (x *Root) replace(path string, perm os.FileMode, fill func(io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := x.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var tmpPath string
	var tmp *os.File
	for i := 0; ; i++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		tmpPath = filepath.Join(dir, "."+filepath.Base(path)+".tmp-"+hex.EncodeToString(b[:]))
		var err error
		// O_EXCL never follows a symlink planted at the temp name.
		tmp, err = x.openFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || i > 8 {
			return err
		}
	}
	defer x.Remove(tmpPath) // no-op after a successful rename
	if err := fill(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return x.rename(tmpPath, path)
}

// host is the unconfined host filesystem.
var host = &Root{}

// WriteFile atomically replaces path (a host path) with data.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return host.WriteFile(path, data, perm)
}

// CopyFile atomically copies src to dst (host paths).
func CopyFile(src, dst string, perm os.FileMode) error {
	return host.CopyFile(src, dst, perm)
}
