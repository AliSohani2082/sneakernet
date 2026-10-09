// Package install copies the bundle into a target system, writes the Xray
// config, sets up the service, and removes it all again on uninstall.
package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/AliSohani2082/sneakernet/internal/bundle"
	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
)

// Options for Install.
type Options struct {
	Bundle *bundle.Bundle
	Target target.Target
	// Svc is nil when the target's init system is not supported; the files
	// are still installed and the user is told how to start Xray.
	Svc   service.Manager
	State manage.State
	// Servers is the server list to install: the stick's servers.txt plus
	// any links the user pasted. It may be empty; then the service is set up
	// but stays off until servers are added (sneakernet tui).
	Servers []byte
	// Log receives progress lines.
	Log func(format string, a ...any)
}

// Manifest records what an install created, so uninstall removes exactly that.
type Manifest struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installedAt"`
	Files       []string  `json:"files"`
	Links       []string  `json:"links"`
	Units       []string  `json:"units"`
	Dirs        []string  `json:"dirs"`
}

// Result of an install.
type Result struct {
	State     manage.State
	Started   bool   // the service is running now
	Enabled   bool   // the service starts at boot
	BootOnly  bool   // the unit lives in /run: gone after a reboot (read-only /etc/systemd/system)
	NoServers bool   // nothing usable to connect to yet
	Manual    string // how to start Xray when there is no service manager
	Command   string // how to run sneakernet: "sneakernet" when on PATH, else the full path
	Manifest  Manifest
}

// Install copies the payload and activates the chosen server. It is safe to
// run again: files are replaced in place.
func Install(ctx context.Context, o Options) (*Result, error) {
	log := o.Log
	if log == nil {
		log = func(string, ...any) {}
	}
	t := o.Target
	m := Manifest{
		Version:     o.Bundle.Version(),
		InstalledAt: time.Now().UTC(),
		Dirs:        []string{layout.OptDir, layout.EtcDir, layout.VarDir},
	}

	// Managed directories must be real, root-owned and not writable by
	// others: we are about to put privileged executables in them.
	for _, d := range []struct {
		path string
		perm os.FileMode
	}{{layout.OptDir, 0o755}, {layout.BinDir, 0o755}, {layout.AssetDir, 0o755}, {layout.EtcDir, 0o700}, {layout.VarDir, 0o755}} {
		if err := t.EnsureDir(d.path, d.perm); err != nil {
			return nil, err
		}
	}

	// Copy the payload into a private directory and check those bytes; only
	// the staged files are installed or executed from here on.
	stage, err := o.Bundle.Stage("")
	if err != nil {
		return nil, err
	}
	defer stage.Cleanup()
	if err := stage.Verify(); err != nil {
		return nil, err
	}
	copies := []struct {
		src, dst string
		perm     os.FileMode
	}{
		{stage.XrayBin(), layout.XrayBin, 0o755},
		{stage.SelfBin(), layout.SelfBin, 0o755},
	}
	for _, g := range layout.GeoFiles {
		copies = append(copies, struct {
			src, dst string
			perm     os.FileMode
		}{filepath.Join(stage.AssetDir(), g), filepath.Join(layout.AssetDir, g), 0o644})
	}
	for _, c := range copies {
		log("copy %s", c.dst)
		if err := t.CopyFile(c.src, c.dst, c.perm); err != nil {
			return nil, fmt.Errorf("copy %s: %w", c.dst, err)
		}
		m.Files = append(m.Files, c.dst)
	}
	log("write %s", layout.ServersFile)
	if err := t.WriteFile(layout.ServersFile, o.Servers, 0o600); err != nil {
		return nil, err
	}
	m.Files = append(m.Files, layout.ServersFile)

	if o.Svc != nil {
		log("create the %s system user", layout.ServiceUser)
		gid, err := service.EnsureUser(t, nil)
		if err != nil {
			return nil, fmt.Errorf("create service user: %w", err)
		}
		m.Files = append(m.Files, layout.SysusersFile)
		if err := t.Chmod(layout.EtcDir, 0o750); err != nil {
			return nil, err
		}
		if err := service.ChownToGroup(t, layout.EtcDir, gid); err != nil {
			return nil, err
		}
	}

	res := &Result{State: o.State, Command: layout.SelfBin}
	osInfo, _ := detect.ReadOS(t.Root)
	if dir, onPath := commandDir(t, osInfo.Family); dir != "" {
		link := filepath.Join(dir, layout.CommandName)
		if err := linkCommand(t, link); err != nil {
			log("note: could not create %s: %v", link, err)
		} else {
			m.Links = append(m.Links, link)
			if onPath {
				res.Command = layout.CommandName
			}
		}
	}

	mgr := &manage.Manager{T: t, Svc: o.Svc, XrayBin: stage.XrayBin(), AssetDir: stage.AssetDir()}
	servers, _, err := mgr.Servers()
	if err != nil {
		return nil, err
	}
	if countUsable(servers) == 0 {
		log("no usable servers yet: save settings, leave the service off")
		res.NoServers = true
		if err := mgr.SaveState(o.State); err != nil {
			return nil, err
		}
	} else {
		log("check the Xray config for %s", o.State.Describe())
		if res.State, err = mgr.Apply(ctx, o.State); err != nil {
			return nil, err
		}
		m.Files = append(m.Files, layout.ConfigFile)
	}
	m.Files = append(m.Files, layout.StateFile)

	restoreSELinuxLabels(t, log)

	if o.Svc == nil {
		res.Manual = fmt.Sprintf("XRAY_LOCATION_ASSET=%s %s run -config %s", layout.AssetDir, layout.XrayBin, layout.ConfigFile)
	} else {
		log("install %s", layout.UnitName)
		if err := o.Svc.Install(layout.UnitName, service.XrayUnit()); err != nil {
			return nil, err
		}
		m.Units = append(m.Units, layout.UnitName)
		if !res.NoServers {
			// On a re-install the service is already running the old config.
			if err := o.Svc.Enable(layout.UnitName); err != nil {
				return nil, err
			}
			if err := o.Svc.Restart(layout.UnitName); err != nil {
				return nil, err
			}
			res.Enabled = true
			res.Started = t.Running
		}
		if sd, ok := o.Svc.(*service.Systemd); ok && !sd.Persistent(layout.UnitName) {
			res.BootOnly = true
		}
	}

	res.Manifest = m
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := t.WriteFile(layout.ManifestFile, append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	return res, nil
}

func countUsable(servers []links.Server) int {
	n := 0
	for _, s := range servers {
		if s.Usable() {
			n++
		}
	}
	return n
}

// commandDir picks the directory for the `sneakernet` link and reports
// whether it is on PATH. See pickCommandDir.
func commandDir(t target.Target, family string) (string, bool) {
	if !t.Running {
		// Another root's PATH is unknown; /usr/local/bin is on it everywhere
		// except NixOS, which has no writable directory on PATH at all.
		if family == "nixos" {
			return "", false
		}
		return filepath.Dir(layout.CommandLink), true
	}
	path := os.Getenv("PATH")
	if family == "nixos" {
		path = nixosWrappers + ":" + path // first on every user's PATH there
	}
	return pickCommandDir(path, family != "nixos", writableDir)
}

// nixosWrappers is a root-writable tmpfs directory that NixOS puts first on
// every PATH. Links there last until the next reboot or nixos-rebuild.
const nixosWrappers = "/run/wrappers/bin"

// pickCommandDir prefers /usr/local/bin when it is on PATH (and preferLocal
// is set). Otherwise it takes the first PATH entry that usable accepts,
// skipping directories owned by the package manager and home directories.
// With nothing suitable it falls back to /usr/local/bin, not on PATH.
func pickCommandDir(path string, preferLocal bool, usable func(string) bool) (string, bool) {
	local := filepath.Dir(layout.CommandLink)
	dirs := filepath.SplitList(path)
	if preferLocal {
		for _, d := range dirs {
			if filepath.Clean(d) == local {
				return local, true
			}
		}
	}
	for _, d := range dirs {
		d = filepath.Clean(d)
		switch {
		case !filepath.IsAbs(d), d == local,
			d == "/usr/bin", d == "/bin", d == "/usr/sbin", d == "/sbin",
			strings.HasPrefix(d, "/nix/"), strings.HasPrefix(d, "/home/"), strings.HasPrefix(d, "/root"):
			continue
		}
		if usable(d) {
			return d, true
		}
	}
	return local, false
}

// writableDir reports whether dir can take a new file: it resolves outside
// the Nix store and is writable (which also fails on read-only mounts).
func writableDir(dir string) bool {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil || strings.HasPrefix(real, "/nix/store/") {
		return false
	}
	return syscall.Access(real, 2 /* W_OK */) == nil
}

// linkCommand links `sneakernet` at link without clobbering a file we did
// not create.
func linkCommand(t target.Target, link string) error {
	path := link
	if fi, err := t.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a symlink", path)
		}
		if err := t.Remove(link); err != nil {
			return err
		}
	}
	if err := t.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return t.Symlink(layout.SelfBin, link)
}

// restoreSELinuxLabels gives copied files the default labels for their paths
// (Fedora/RHEL); without it systemd may refuse to execute the binaries.
func restoreSELinuxLabels(t target.Target, log func(string, ...any)) {
	if !t.Running {
		return
	}
	if _, err := os.Stat("/sys/fs/selinux/enforce"); err != nil {
		return
	}
	if _, err := exec.LookPath("restorecon"); err != nil {
		return
	}
	if out, err := exec.Command("restorecon", "-R", layout.OptDir, layout.EtcDir).CombinedOutput(); err != nil {
		log("note: restorecon: %v: %s", err, out)
	}
}

// Uninstall removes everything an install created. It only ever deletes
// sneakernet's own directories and the links and units in the manifest.
func Uninstall(t target.Target, svc service.Manager, log func(string, ...any)) error {
	if log == nil {
		log = func(string, ...any) {}
	}
	m := Manifest{Links: []string{layout.CommandLink}, Units: []string{layout.UnitName}}
	if b, err := t.ReadFile(layout.ManifestFile); err == nil {
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("%s: %w", layout.ManifestFile, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	var errs []error
	for _, u := range m.Units {
		if u != layout.UnitName || svc == nil {
			continue
		}
		log("remove %s", u)
		errs = append(errs, svc.Remove(u))
	}
	// Only a user we created: our sysusers entry is in /etc or /run.
	for _, conf := range []string{layout.SysusersFile, layout.RuntimeSysusersFile} {
		if _, err := os.Stat(t.Path(conf)); err == nil {
			log("remove the %s system user", layout.ServiceUser)
			service.RemoveUser(t, nil)
			break
		}
	}
	for _, l := range m.Links {
		if dst, err := t.Readlink(l); err == nil && dst == layout.SelfBin {
			log("remove %s", l)
			errs = append(errs, t.Remove(l))
		}
	}
	// Only our own directories, whatever the manifest says.
	for _, d := range []string{layout.OptDir, layout.EtcDir, layout.VarDir} {
		log("remove %s", d)
		errs = append(errs, t.RemoveAll(d))
	}
	return errors.Join(errs...)
}
