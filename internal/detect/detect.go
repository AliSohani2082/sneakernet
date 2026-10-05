// Package detect inspects the host and the install target: distribution,
// init system, libc, live session and CPU architecture.
package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// OS is the parsed os-release of a root filesystem.
type OS struct {
	ID         string
	IDLike     []string
	PrettyName string
	VersionID  string
	Family     string // debian | fedora | arch | suse | alpine | nixos | other
}

// ReadOS parses <root>/etc/os-release (falling back to /usr/lib/os-release).
func ReadOS(root string) (OS, error) {
	var (
		f   *os.File
		err error
	)
	for _, p := range []string{"etc/os-release", "usr/lib/os-release"} {
		if f, err = os.Open(filepath.Join(root, p)); err == nil {
			break
		}
	}
	if err != nil {
		return OS{Family: "other", PrettyName: "unknown Linux"}, err
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		kv[k] = strings.Trim(v, `"'`)
	}
	o := OS{
		ID:         strings.ToLower(kv["ID"]),
		IDLike:     strings.Fields(strings.ToLower(kv["ID_LIKE"])),
		PrettyName: kv["PRETTY_NAME"],
		VersionID:  kv["VERSION_ID"],
	}
	if o.PrettyName == "" {
		o.PrettyName = kv["NAME"]
	}
	o.Family = family(o.ID, o.IDLike)
	return o, sc.Err()
}

func family(id string, like []string) string {
	families := map[string]string{
		"debian": "debian", "ubuntu": "debian",
		"fedora": "fedora", "rhel": "fedora", "centos": "fedora",
		"arch": "arch",
		"suse": "suse", "opensuse": "suse",
		"alpine": "alpine",
		"nixos":  "nixos",
	}
	for _, c := range append([]string{id}, like...) {
		if f, ok := families[c]; ok {
			return f
		}
		if strings.HasPrefix(c, "opensuse") {
			return "suse"
		}
	}
	return "other"
}

// Init systems.
const (
	Systemd = "systemd"
	OpenRC  = "openrc"
	Runit   = "runit"
	Unknown = "unknown"
)

// InitSystem reports the init system of the target root. For the running
// system it trusts the runtime markers; for a mounted disk it looks at what
// /sbin/init points to.
func InitSystem(root string, running bool) string {
	exists := func(p string) bool {
		_, err := os.Lstat(filepath.Join(root, p))
		return err == nil
	}
	if running {
		switch {
		case exists("run/systemd/system"):
			return Systemd
		case exists("run/openrc"):
			return OpenRC
		case exists("run/runit"):
			return Runit
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "sbin/init")); err == nil {
		switch {
		case strings.Contains(target, "systemd"):
			return Systemd
		case strings.Contains(target, "openrc"):
			return OpenRC
		case strings.Contains(target, "runit"):
			return Runit
		}
	}
	switch {
	case exists("usr/lib/systemd/systemd"), exists("lib/systemd/systemd"):
		return Systemd
	case exists("sbin/openrc-run"), exists("usr/sbin/openrc-run"):
		return OpenRC
	case exists("usr/bin/runit"), exists("sbin/runit"):
		return Runit
	}
	return Unknown
}

// Libc is "musl" or "glibc".
func Libc(root string) string {
	if m, _ := filepath.Glob(filepath.Join(root, "lib/ld-musl-*.so.1")); len(m) > 0 {
		return "musl"
	}
	return "glibc"
}

// liveMarkers are kernel command-line words that live ISOs boot with.
var liveMarkers = []string{
	"boot=casper", "boot=live", "rd.live.image", "archisobasedir=", "archisolabel=",
	"root=live:", "cdroot", "findiso=", "iso-scan/filename=",
}

// LiveSession reports whether the running system booted from a live image.
func LiveSession() bool {
	b, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return false
	}
	return isLiveCmdline(string(b))
}

func isLiveCmdline(cmdline string) bool {
	for _, w := range strings.Fields(cmdline) {
		for _, m := range liveMarkers {
			if w == m || (strings.HasSuffix(m, "=") && strings.HasPrefix(w, m)) {
				return true
			}
		}
	}
	return false
}

// Arch is the bundle architecture name of this binary: amd64, arm64, 386 or
// armv7. The bootstrap picked this binary from `uname -m`, so it matches the
// running CPU.
func Arch() string {
	if runtime.GOARCH == "arm" {
		return "armv7"
	}
	return runtime.GOARCH
}

// Desktop returns the desktop environment name, if any.
func Desktop() string {
	return os.Getenv("XDG_CURRENT_DESKTOP")
}
