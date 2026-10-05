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
	"time"

	"github.com/AliSohani2082/sneakernet/internal/bundle"
	"github.com/AliSohani2082/sneakernet/internal/fsutil"
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
	NoServers bool   // nothing usable to connect to yet
	Manual    string // how to start Xray when there is no service manager
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

	for _, d := range []struct {
		path string
		perm os.FileMode
	}{{layout.BinDir, 0o755}, {layout.AssetDir, 0o755}, {layout.EtcDir, 0o700}, {layout.VarDir, 0o755}} {
		if err := os.MkdirAll(t.Path(d.path), d.perm); err != nil {
			return nil, err
		}
	}
	copies := []struct {
		src, dst string
		perm     os.FileMode
	}{
		{o.Bundle.XrayBin(), layout.XrayBin, 0o755},
		{o.Bundle.SelfBin(), layout.SelfBin, 0o755},
	}
	for _, g := range layout.GeoFiles {
		copies = append(copies, struct {
			src, dst string
			perm     os.FileMode
		}{filepath.Join(o.Bundle.AssetDir(), g), filepath.Join(layout.AssetDir, g), 0o644})
	}
	for _, c := range copies {
		log("copy %s", c.dst)
		if err := fsutil.CopyFile(c.src, t.Path(c.dst), c.perm); err != nil {
			return nil, fmt.Errorf("copy %s: %w", c.dst, err)
		}
		m.Files = append(m.Files, c.dst)
	}
	log("write %s", layout.ServersFile)
	if err := fsutil.WriteFile(t.Path(layout.ServersFile), o.Servers, 0o600); err != nil {
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
		if err := os.Chmod(t.Path(layout.EtcDir), 0o750); err != nil {
			return nil, err
		}
		if err := service.ChownToGroup(t.Path(layout.EtcDir), gid); err != nil {
			return nil, err
		}
	}

	if err := linkCommand(t); err != nil {
		log("note: could not create %s: %v", layout.CommandLink, err)
	} else {
		m.Links = append(m.Links, layout.CommandLink)
	}

	mgr := &manage.Manager{T: t, Svc: o.Svc}
	res := &Result{State: o.State}
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
	}

	res.Manifest = m
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := fsutil.WriteFile(t.Path(layout.ManifestFile), append(b, '\n'), 0o644); err != nil {
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

// linkCommand puts `sneakernet` on PATH without clobbering a file we did not create.
func linkCommand(t target.Target) error {
	link := t.Path(layout.CommandLink)
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s exists and is not a symlink", layout.CommandLink)
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(layout.SelfBin, link)
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
	if b, err := os.ReadFile(t.Path(layout.ManifestFile)); err == nil {
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
	if _, err := os.Stat(t.Path(layout.SysusersFile)); err == nil {
		log("remove the %s system user", layout.ServiceUser)
		service.RemoveUser(t, nil)
	}
	for _, l := range m.Links {
		p := t.Path(l)
		if dst, err := os.Readlink(p); err == nil && dst == layout.SelfBin {
			log("remove %s", l)
			errs = append(errs, os.Remove(p))
		}
	}
	// Only our own directories, whatever the manifest says.
	for _, d := range []string{layout.OptDir, layout.EtcDir, layout.VarDir} {
		log("remove %s", d)
		errs = append(errs, os.RemoveAll(t.Path(d)))
	}
	return errors.Join(errs...)
}
