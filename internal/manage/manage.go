// Package manage operates an installed sneakernet: it keeps the chosen
// server and routing in a state file and regenerates the Xray config from it.
package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/AliSohani2082/sneakernet/internal/fsutil"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
	"github.com/AliSohani2082/sneakernet/internal/xrayconf"
)

// State is what the user chose; the Xray config is derived from it.
type State struct {
	Auto      bool   `json:"auto"`
	Index     int    `json:"index,omitempty"` // links.Server.Index when !Auto
	Name      string `json:"name,omitempty"`  // server name at the time it was chosen
	Routing   string `json:"routing"`
	Region    string `json:"region,omitempty"`
	SocksPort int    `json:"socksPort"`
	HTTPPort  int    `json:"httpPort"`
}

// DefaultState picks the fastest working server and keeps LAN traffic local.
func DefaultState() State {
	return State{Auto: true, Routing: xrayconf.RoutingBypassLAN, SocksPort: 10808, HTTPPort: 10809}
}

func (s State) Selection() xrayconf.Selection {
	return xrayconf.Selection{Auto: s.Auto, Index: s.Index}
}

func (s State) Options() xrayconf.Options {
	return xrayconf.Options{SocksPort: s.SocksPort, HTTPPort: s.HTTPPort, Routing: s.Routing, Region: s.Region}
}

// Describe is a one-line summary, e.g. `#12 "DE reality"` or `auto`.
func (s State) Describe() string {
	if s.Auto {
		return "auto (fastest working server)"
	}
	return fmt.Sprintf("#%d %q", s.Index, s.Name)
}

// Manager reads and writes an installation on a target.
type Manager struct {
	T   target.Target
	Svc service.Manager // nil on unsupported init systems
	// SkipValidate disables `xray run -test`, for targets whose Xray binary
	// cannot run on this host (another CPU architecture).
	SkipValidate bool
}

// Servers parses the installed server list.
func (m *Manager) Servers() ([]links.Server, []links.LineError, error) {
	f, err := os.Open(m.T.Path(layout.ServersFile))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return links.ParseList(f)
}

// State reads the saved state; a missing file yields DefaultState and an
// error wrapping os.ErrNotExist.
func (m *Manager) State() (State, error) {
	st := DefaultState()
	b, err := os.ReadFile(m.T.Path(layout.StateFile))
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return DefaultState(), fmt.Errorf("%s: %w", layout.StateFile, err)
	}
	return st, nil
}

// Apply builds the config for st, checks it with Xray, and writes config and
// state. It does not restart the service; see Switch.
func (m *Manager) Apply(ctx context.Context, st State) (State, error) {
	servers, _, err := m.Servers()
	if err != nil {
		return st, err
	}
	if !st.Auto {
		st.Name = ""
		for _, s := range servers {
			if s.Index == st.Index {
				st.Name = s.Name
			}
		}
	}
	cfg, err := xrayconf.Build(servers, st.Selection(), st.Options())
	if err != nil {
		return st, err
	}
	b, err := cfg.JSON()
	if err != nil {
		return st, err
	}
	if !m.SkipValidate {
		if err := xrayconf.Validate(ctx, m.T.Path(layout.XrayBin), m.T.Path(layout.AssetDir), b); err != nil {
			return st, err
		}
	}
	if err := os.MkdirAll(m.T.Path(layout.EtcDir), 0o700); err != nil {
		return st, err
	}
	// The service user reads the config through its group; without that
	// user (no systemd) only root may read it.
	perm := os.FileMode(0o600)
	gid, gerr := service.GroupID(m.T, layout.ServiceUser)
	if gerr == nil {
		perm = 0o640
	}
	cfgPath := m.T.Path(layout.ConfigFile)
	if err := fsutil.WriteFile(cfgPath, b, perm); err != nil {
		return st, err
	}
	if gerr == nil {
		if err := service.ChownToGroup(cfgPath, gid); err != nil {
			return st, err
		}
	}
	sb, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	return st, fsutil.WriteFile(m.T.Path(layout.StateFile), append(sb, '\n'), 0o600)
}

// Switch applies st and restarts the service so it takes effect.
func (m *Manager) Switch(ctx context.Context, st State) (State, error) {
	st, err := m.Apply(ctx, st)
	if err != nil {
		return st, err
	}
	if m.Svc == nil {
		return st, errors.New("config written, but no supported service manager: restart Xray yourself")
	}
	return st, m.Svc.Restart(layout.UnitName)
}

// Status of the Xray service.
func (m *Manager) Status() (service.Status, error) {
	if m.Svc == nil {
		return service.Status{State: "unmanaged (no systemd)"}, nil
	}
	return m.Svc.Status(layout.UnitName)
}

// Installed reports whether sneakernet is installed on the target.
func (m *Manager) Installed() bool {
	_, err := os.Stat(m.T.Path(layout.StateFile))
	return err == nil
}
