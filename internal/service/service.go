// Package service installs and controls the Xray background service.
//
// Only systemd is supported in this release. On other init systems the
// installer still copies everything and prints how to start Xray by hand.
package service

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/target"
)

//go:embed units/sneakernet-xray.service
var xrayUnit []byte

// XrayUnit is the systemd unit for the Xray client.
func XrayUnit() []byte { return xrayUnit }

// ErrUnsupportedInit is returned for init systems other than systemd.
var ErrUnsupportedInit = errors.New("only systemd is supported for now")

// Status is a service's current state.
type Status struct {
	Active  bool
	State   string // e.g. "active (running)", "failed", "inactive (dead)"
	Since   string // when it entered the active state
	Enabled bool
}

// Manager controls services on a target.
type Manager interface {
	Install(name string, unit []byte) error
	Enable(name string) error // enable at boot; also start when the target is running
	Restart(name string) error
	Stop(name string) error
	Start(name string) error
	Remove(name string) error // stop, disable and delete the unit
	Status(name string) (Status, error)
	Logs(name string, lines int) (string, error)
}

// Runner executes a command and returns its combined output. Tests replace it.
type Runner func(name string, args ...string) ([]byte, error)

func execRunner(name string, args ...string) ([]byte, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// For returns the service manager for the target's init system.
func For(t target.Target, init string) (Manager, error) {
	if init != detect.Systemd {
		return nil, fmt.Errorf("%w (found %s)", ErrUnsupportedInit, init)
	}
	return &Systemd{T: t, Run: execRunner}, nil
}

// Systemd manages units with systemctl. For a target that is not the running
// system it only writes and enables units (systemctl --root).
type Systemd struct {
	T   target.Target
	Run Runner
}

func (s *Systemd) unitPath(name string) string {
	return s.T.Path(filepath.Join(layout.UnitDir, name))
}

func (s *Systemd) runtimePath(name string) string {
	return s.T.Path(filepath.Join(layout.RuntimeUnitDir, name))
}

// Persistent reports whether the unit survives a reboot. It is false when
// the unit had to go to /run/systemd/system because /etc/systemd/system is
// read-only.
func (s *Systemd) Persistent(name string) bool {
	_, err := os.Stat(s.runtimePath(name))
	return err != nil
}

func (s *Systemd) systemctl(args ...string) ([]byte, error) {
	if !s.T.Running {
		args = append([]string{"--root=" + s.T.Root}, args...)
	}
	return s.Run("systemctl", args...)
}

// Install writes the unit to /etc/systemd/system. When that directory is
// read-only (NixOS links it into the Nix store) and the target is the running
// system, the unit goes to /run/systemd/system instead, for this boot only.
func (s *Systemd) Install(name string, unit []byte) error {
	err := s.writeUnit(filepath.Join(layout.UnitDir, name), unit)
	if err != nil && s.T.Running && ReadOnly(err) {
		err = s.writeUnit(filepath.Join(layout.RuntimeUnitDir, name), unit)
	} else if err == nil {
		_ = s.T.Remove(filepath.Join(layout.RuntimeUnitDir, name)) // a writable /etc wins over an old runtime copy
	}
	if err != nil {
		return err
	}
	if s.T.Running {
		_, err := s.systemctl("daemon-reload")
		return err
	}
	return nil
}

// writeUnit replaces the unit atomically and never writes through a symlink
// (a planted unit -> /etc/shadow would otherwise be truncated), and stays
// inside the target root.
func (s *Systemd) writeUnit(path string, data []byte) error {
	return s.T.WriteFile(path, data, 0o644)
}

// ReadOnly reports whether err means a file could not be written because
// the directory or filesystem is read-only.
func ReadOnly(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS)
}

func (s *Systemd) Enable(name string) error {
	if !s.T.Running {
		_, err := s.systemctl("enable", name)
		return err
	}
	args := []string{"enable", "--now", name}
	if !s.Persistent(name) {
		args = []string{"enable", "--runtime", "--now", name}
	}
	_, err := s.systemctl(args...)
	return err
}

func (s *Systemd) Restart(name string) error { return s.live("restart", name) }
func (s *Systemd) Stop(name string) error    { return s.live("stop", name) }
func (s *Systemd) Start(name string) error   { return s.live("start", name) }

// live runs a command that only makes sense on the running system.
func (s *Systemd) live(verb, name string) error {
	if !s.T.Running {
		return nil
	}
	_, err := s.systemctl(verb, name)
	return err
}

func (s *Systemd) Remove(name string) error {
	switch {
	case !s.T.Running:
		_, _ = s.systemctl("disable", name)
	case s.Persistent(name):
		_, _ = s.systemctl("disable", "--now", name)
	default:
		_, _ = s.systemctl("disable", "--runtime", "--now", name)
	}
	for _, p := range []string{filepath.Join(layout.UnitDir, name), filepath.Join(layout.RuntimeUnitDir, name)} {
		// Check first: on a read-only filesystem even removing a missing
		// file fails (EROFS), not just with "not found".
		if _, err := s.T.Lstat(p); err != nil {
			continue
		}
		if err := s.T.Remove(p); err != nil {
			return err
		}
	}
	if s.T.Running {
		_, _ = s.systemctl("daemon-reload")
		_, _ = s.systemctl("reset-failed", name)
	}
	return nil
}

func (s *Systemd) Status(name string) (Status, error) {
	if !s.T.Running {
		return Status{State: "not running (installed system)"}, nil
	}
	out, err := s.systemctl("show", name, "--property=ActiveState,SubState,ActiveEnterTimestamp,UnitFileState")
	if err != nil {
		return Status{}, err
	}
	p := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			p[k] = v
		}
	}
	st := Status{
		Active:  p["ActiveState"] == "active",
		State:   p["ActiveState"],
		Enabled: p["UnitFileState"] == "enabled",
	}
	if p["SubState"] != "" {
		st.State += " (" + p["SubState"] + ")"
	}
	if st.Active {
		st.Since = p["ActiveEnterTimestamp"]
	}
	return st, nil
}

func (s *Systemd) Logs(name string, lines int) (string, error) {
	if !s.T.Running {
		return "", nil
	}
	out, err := s.Run("journalctl", "--unit", name, "--lines", fmt.Sprint(lines), "--no-pager", "--output", "short-iso")
	return string(out), err
}
