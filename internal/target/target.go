// Package target abstracts where sneakernet installs: the running system, or
// another root filesystem (an installed system on disk, or a test directory).
package target

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/AliSohani2082/sneakernet/internal/fsutil"
)

// Target is a root filesystem to install into.
type Target struct {
	// Root is "/" for the running system.
	Root string
	// Running means the target is the booted system, so services can be
	// started now. For any other root they are only enabled for next boot.
	Running bool
}

// RunningSystem is the booted system.
func RunningSystem() Target { return Target{Root: "/", Running: true} }

// Dir is another root filesystem, for example an installed system mounted
// at /mnt/target.
func Dir(root string) Target { return Target{Root: root} }

// Path maps an absolute path in the target to a host path. Use it for
// reading and for handing paths to other programs; privileged writes go
// through the methods below, which never leave the target root.
func (t Target) Path(p string) string { return filepath.Join(t.Root, p) }

// do runs fn on a root opened for this target. For a target other than "/"
// the root is confined: a symlink pointing outside it is an error.
func (t Target) do(fn func(*fsutil.Root) error) error {
	x, err := fsutil.OpenRoot(t.Root)
	if err != nil {
		return err
	}
	defer x.Close()
	return fn(x)
}

// EnsureDir creates a directory managed by sneakernet and checks it is a
// real directory, root-owned and not writable by group or others.
func (t Target) EnsureDir(p string, perm os.FileMode) error {
	return t.do(func(x *fsutil.Root) error { return x.EnsureDir(p, perm) })
}

// MkdirAll creates p and its parents inside the target.
func (t Target) MkdirAll(p string, perm os.FileMode) error {
	return t.do(func(x *fsutil.Root) error { return x.MkdirAll(p, perm) })
}

// WriteFile atomically replaces p inside the target.
func (t Target) WriteFile(p string, data []byte, perm os.FileMode) error {
	return t.do(func(x *fsutil.Root) error { return x.WriteFile(p, data, perm) })
}

// CopyFile atomically copies a host file src to p inside the target.
func (t Target) CopyFile(src, p string, perm os.FileMode) error {
	return t.do(func(x *fsutil.Root) error { return x.CopyFile(src, p, perm) })
}

// Chmod changes the mode of p inside the target.
func (t Target) Chmod(p string, perm os.FileMode) error {
	return t.do(func(x *fsutil.Root) error { return x.Chmod(p, perm) })
}

// Chown changes the owner of p inside the target.
func (t Target) Chown(p string, uid, gid int) error {
	return t.do(func(x *fsutil.Root) error { return x.Chown(p, uid, gid) })
}

// Remove deletes p (a final symlink is removed, not followed).
func (t Target) Remove(p string) error {
	return t.do(func(x *fsutil.Root) error { return x.Remove(p) })
}

// RemoveAll deletes p and everything below it.
func (t Target) RemoveAll(p string) error {
	return t.do(func(x *fsutil.Root) error { return x.RemoveAll(p) })
}

// Symlink creates link pointing at dest.
func (t Target) Symlink(dest, link string) error {
	return t.do(func(x *fsutil.Root) error { return x.Symlink(dest, link) })
}

// Lstat is os.Lstat inside the target.
func (t Target) Lstat(p string) (fi fs.FileInfo, err error) {
	err = t.do(func(x *fsutil.Root) (e error) { fi, e = x.Lstat(p); return })
	return
}

// Readlink is os.Readlink inside the target.
func (t Target) Readlink(p string) (dest string, err error) {
	err = t.do(func(x *fsutil.Root) (e error) { dest, e = x.Readlink(p); return })
	return
}

// ReadFile reads p inside the target.
func (t Target) ReadFile(p string) (b []byte, err error) {
	err = t.do(func(x *fsutil.Root) (e error) { b, e = x.ReadFile(p); return })
	return
}
