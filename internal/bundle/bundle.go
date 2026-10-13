// Package bundle locates and verifies the offline payload on the USB stick.
//
// Layout of a bundle directory:
//
//	install.sh  uninstall.sh  README.txt  VERSION  SHA256SUMS
//	servers.txt                       the user's server list (may be missing)
//	bin/<arch>/{sneakernet,xray}
//	data/{geoip.dat,geosite.dat}
//
// SHA256SUMS covers bin/, data/ and the shell scripts. servers.txt is meant
// to be edited on the stick, so it is not checksummed.
//
// SHA256SUMS lives on the same media as the files it lists, so it detects
// corruption and careless edits only. Someone who can rewrite the stick can
// rewrite the sums too; see docs/SECURITY.md.
package bundle

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/AliSohani2082/sneakernet/internal/layout"
)

// maxFileSize bounds every payload file read. The largest real file (the Xray
// binary) is a few tens of MB; a symlink to /dev/zero or a huge file must not
// hang or fill the disk.
const maxFileSize = 256 << 20

// maxSumsSize bounds SHA256SUMS itself.
const maxSumsSize = 1 << 20

// SumsFile lists "<sha256>  <relative path>" for every payload file.
const SumsFile = "SHA256SUMS"

// Bundle is a payload directory for one CPU architecture.
type Bundle struct {
	Dir  string
	Arch string
	// SkipVerify makes Stage copy files without checking SHA256SUMS
	// (--skip-verify, development only).
	SkipVerify bool
}

// Open checks that dir looks like a bundle with binaries for arch.
func Open(dir, arch string) (*Bundle, error) {
	b := &Bundle{Dir: dir, Arch: arch}
	if _, err := os.Stat(filepath.Join(dir, SumsFile)); err != nil {
		return nil, fmt.Errorf("%s is not a sneakernet bundle (no %s)", dir, SumsFile)
	}
	if _, err := os.Stat(b.XrayBin()); err != nil {
		return nil, fmt.Errorf("this bundle has no Xray core for %s", arch)
	}
	return b, nil
}

func (b *Bundle) path(p ...string) string { return filepath.Join(append([]string{b.Dir}, p...)...) }

func (b *Bundle) XrayBin() string     { return b.path("bin", b.Arch, "xray") }
func (b *Bundle) SelfBin() string     { return b.path("bin", b.Arch, "sneakernet") }
func (b *Bundle) AssetDir() string    { return b.path("data") }
func (b *Bundle) ServersFile() string { return b.path("servers.txt") }

// Version is the bundle's VERSION file, or "unknown".
func (b *Bundle) Version() string {
	v, err := os.ReadFile(b.path("VERSION"))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(v))
}

// Corrupt lists files whose checksum does not match.
type Corrupt struct {
	Files []string
}

func (c *Corrupt) Error() string {
	shown := c.Files
	if len(shown) > 5 {
		shown = append(shown[:5:5], fmt.Sprintf("and %d more", len(c.Files)-5))
	}
	return "bundle files are missing or corrupt (re-copy the folder to the stick): " + strings.Join(shown, ", ")
}

// Required lists the payload files an install copies for this architecture
// (bundle-relative, slash separated). SHA256SUMS must list every one.
func (b *Bundle) Required() []string {
	req := []string{"bin/" + b.Arch + "/xray", "bin/" + b.Arch + "/sneakernet"}
	for _, g := range layout.GeoFiles {
		req = append(req, "data/"+g)
	}
	return req
}

// manifest parses SHA256SUMS strictly: well-formed lines, local clean paths,
// no duplicates, and an entry for every required file. The result maps
// slash-separated relative paths to hex digests.
func (b *Bundle) manifest() (map[string]string, error) {
	f, err := os.Open(b.path(SumsFile))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sums := map[string]string{}
	sc := bufio.NewScanner(io.LimitReader(f, maxSumsSize))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		sum, rel, ok := strings.Cut(line, "  ")
		if !ok || !validDigest(sum) {
			return nil, fmt.Errorf("%s: malformed line %q", SumsFile, line)
		}
		rel = strings.TrimPrefix(rel, "*")
		if !safeRel(rel) {
			return nil, fmt.Errorf("%s: unsafe path %q", SumsFile, rel)
		}
		if _, dup := sums[rel]; dup {
			return nil, fmt.Errorf("%s: %q is listed twice", SumsFile, rel)
		}
		sums[rel] = sum
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for _, rel := range b.Required() {
		if _, ok := sums[rel]; !ok {
			return nil, fmt.Errorf("%s does not list %s: the bundle is incomplete", SumsFile, rel)
		}
	}
	return sums, nil
}

func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// safeRel accepts only clean, relative, slash-separated paths that stay
// inside the bundle.
func safeRel(rel string) bool {
	return rel != "" && !strings.Contains(rel, "\\") && path.Clean(rel) == rel && filepath.IsLocal(filepath.FromSlash(rel))
}

// Verify checks SHA256SUMS: it must list every file an install copies, and
// every listed file that this architecture needs must match. Files for other
// architectures are skipped, so a stick with a damaged arm64 binary still
// installs on x86_64. Verify alone does not protect against the stick
// changing afterwards; Stage binds the verified bytes to the install.
func (b *Bundle) Verify() error {
	sums, err := b.manifest()
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(b.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	bad := &Corrupt{}
	for rel, sum := range sums {
		if !b.needs(filepath.FromSlash(rel)) {
			continue
		}
		f, err := openPayload(root, rel)
		if err == nil {
			var got string
			got, err = hashReader(f)
			f.Close()
			if err == nil && got != sum {
				err = errors.New("checksum mismatch")
			}
		}
		if err != nil {
			bad.Files = append(bad.Files, filepath.FromSlash(rel))
		}
	}
	if len(bad.Files) > 0 {
		sort.Strings(bad.Files)
		return bad
	}
	return nil
}

func (b *Bundle) needs(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return !(len(parts) > 2 && parts[0] == "bin" && parts[1] != b.Arch)
}

// openPayload opens a regular file below root. Every path component must be
// a real directory or file: symlinks are rejected, as is anything that is not
// a regular file (devices, FIFOs).
func openPayload(root *os.Root, rel string) (*os.File, error) {
	parts := strings.Split(rel, "/")
	for i := range parts {
		fi, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink", rel)
		}
		if i == len(parts)-1 && !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", rel)
		}
	}
	// O_NONBLOCK: even if the file is swapped for a FIFO after the Lstat,
	// opening it must not hang.
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s is not a regular file", rel)
	}
	if fi.Size() > maxFileSize {
		f.Close()
		return nil, fmt.Errorf("%s is larger than %d bytes", rel, maxFileSize)
	}
	return f, nil
}

// hashReader hashes at most maxFileSize bytes.
func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, maxFileSize+1))
	if err != nil {
		return "", err
	}
	if n > maxFileSize {
		return "", errors.New("file too large")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Staged is a private copy of the payload that an install runs from. The
// directory is created 0700 by the installing user (root), so nobody else
// can change the files after their hashes were checked.
type Staged struct {
	Dir  string
	Arch string

	sums map[string]string // staged file name -> digest; nil when unverified
}

func (s *Staged) XrayBin() string  { return filepath.Join(s.Dir, "xray") }
func (s *Staged) SelfBin() string  { return filepath.Join(s.Dir, "sneakernet") }
func (s *Staged) AssetDir() string { return filepath.Join(s.Dir, "data") }

// Verify hashes the staged files again. Install calls it right before using
// them, so a change to the staging directory is caught, not installed.
func (s *Staged) Verify() error {
	bad := &Corrupt{}
	for dst, want := range s.sums {
		f, err := openRegular(dst)
		if err == nil {
			var got string
			got, err = hashReader(f)
			f.Close()
			if err == nil && got != want {
				err = errors.New("checksum mismatch")
			}
		}
		if err != nil {
			bad.Files = append(bad.Files, filepath.Base(dst))
		}
	}
	if len(bad.Files) > 0 {
		sort.Strings(bad.Files)
		return bad
	}
	return nil
}

func openRegular(p string) (*os.File, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	return f, nil
}

// Cleanup removes the staging directory.
func (s *Staged) Cleanup() { _ = os.RemoveAll(s.Dir) }

// Stage copies exactly the files an install uses into a fresh private
// directory under tmp ("" for the default temp dir), then hashes the copied
// bytes against SHA256SUMS. Install and run only the staged files: that
// closes the gap between checking the stick and reading it again, which
// someone with write access to the stick could otherwise use to swap a
// verified binary for another one.
func (b *Bundle) Stage(tmp string) (*Staged, error) {
	var sums map[string]string
	if !b.SkipVerify {
		var err error
		if sums, err = b.manifest(); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(b.Dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := os.MkdirTemp(tmp, "sneakernet-stage-")
	if err != nil {
		return nil, err
	}
	st := &Staged{Dir: dir, Arch: b.Arch}
	st.sums = map[string]string{}
	ok := false
	defer func() {
		if !ok {
			st.Cleanup()
		}
	}()
	if err := os.Mkdir(st.AssetDir(), 0o700); err != nil {
		return nil, err
	}
	bad := &Corrupt{}
	for _, rel := range b.Required() {
		dst := filepath.Join(dir, strings.TrimPrefix(filepath.ToSlash(rel), "bin/"+b.Arch+"/"))
		perm := os.FileMode(0o600)
		if strings.HasPrefix(rel, "bin/") {
			perm = 0o700
		}
		if err := stageFile(root, rel, dst, perm); err != nil {
			bad.Files = append(bad.Files, filepath.FromSlash(rel))
			continue
		}
		if b.SkipVerify {
			delete(st.sums, dst)
			continue
		}
		st.sums[dst] = sums[rel]
		// Hash what was actually copied, not what is on the stick now.
		f, err := openRegular(dst)
		if err != nil {
			return nil, err
		}
		got, err := hashReader(f)
		f.Close()
		if err != nil || got != sums[rel] {
			bad.Files = append(bad.Files, filepath.FromSlash(rel))
		}
	}
	if len(bad.Files) > 0 {
		return nil, bad
	}
	ok = true
	return st, nil
}

func stageFile(root *os.Root, rel, dst string, perm os.FileMode) error {
	in, err := openPayload(root, rel)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(in, maxFileSize+1))
	if err == nil && n > maxFileSize {
		err = errors.New("file too large")
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}
