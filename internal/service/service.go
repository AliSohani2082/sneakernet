// Package service installs and controls the Xray background service.
//
// Only systemd is supported in this release. On other init systems the
// installer still copies everything and prints how to start Xray by hand.
package service

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

func (s *Systemd) systemctl(args ...string) ([]byte, error) {
	if !s.T.Running {
		args = append([]string{"--root=" + s.T.Root}, args...)
	}
	return s.Run("systemctl", args...)
}

func (s *Systemd) Install(name string, unit []byte) error {
	p := s.unitPath(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, unit, 0o644); err != nil {
		return err
	}
	if s.T.Running {
		_, err := s.systemctl("daemon-reload")
		return err
	}
	return nil
}

func (s *Systemd) Enable(name string) error {
	if s.T.Running {
		_, err := s.systemctl("enable", "--now", name)
		return err
	}
	_, err := s.systemctl("enable", name)
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
	if s.T.Running {
		_, _ = s.systemctl("disable", "--now", name)
	} else {
		_, _ = s.systemctl("disable", name)
	}
	if err := os.Remove(s.unitPath(name)); err != nil && !os.IsNotExist(err) {
		return err
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
