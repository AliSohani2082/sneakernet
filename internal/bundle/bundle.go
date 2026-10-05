// Package bundle locates and verifies the offline payload on the USB stick.
//
// Layout of a bundle directory:
//
//	install.sh  uninstall.sh  README.txt  VERSION  SHA256SUMS
//	servers.txt                       the user's server list (may be missing)
//	bin/<arch>/{sneakernet,xray}
//	data/{geoip.dat,geosite.dat}
//
// SHA256SUMS covers bin/ and data/. servers.txt is meant to be edited on the
// stick, so it is not checksummed.
package bundle

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SumsFile lists "<sha256>  <relative path>" for every payload file.
const SumsFile = "SHA256SUMS"

// Bundle is a payload directory for one CPU architecture.
type Bundle struct {
	Dir  string
	Arch string
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

// Verify hashes every file in SHA256SUMS that this architecture needs. Files
// for other architectures are skipped, so a stick with a damaged arm64 binary
// still installs on x86_64.
func (b *Bundle) Verify() error {
	f, err := os.Open(b.path(SumsFile))
	if err != nil {
		return err
	}
	defer f.Close()
	bad := &Corrupt{}
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		sum, rel, ok := strings.Cut(line, "  ")
		if !ok || len(sum) != 64 {
			return fmt.Errorf("%s: malformed line %q", SumsFile, line)
		}
		rel = filepath.FromSlash(strings.TrimPrefix(rel, "*"))
		if !b.needs(rel) {
			continue
		}
		n++
		if got, err := hashFile(b.path(rel)); err != nil || got != sum {
			bad.Files = append(bad.Files, rel)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if n == 0 {
		return errors.New(SumsFile + " lists no files")
	}
	if len(bad.Files) > 0 {
		return bad
	}
	return nil
}

func (b *Bundle) needs(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return !(len(parts) > 2 && parts[0] == "bin" && parts[1] != b.Arch)
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
