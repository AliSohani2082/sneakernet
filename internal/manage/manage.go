// Package manage operates an installed sneakernet: it keeps the server list
// and the user's choices, and regenerates the Xray config from them.
package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
	"github.com/AliSohani2082/sneakernet/internal/xrayconf"
)

// State is what the user chose; the Xray config is derived from it.
type State struct {
	Auto      bool   `json:"auto"`
	Key       string `json:"key,omitempty"`   // links.Server.Key of the chosen server
	Index     int    `json:"index,omitempty"` // its position when chosen (display only)
	Name      string `json:"name,omitempty"`
	Routing   string `json:"routing"`
	Region    string `json:"region,omitempty"`
	SocksPort int    `json:"socksPort"`
	HTTPPort  int    `json:"httpPort"`
}

// DefaultState picks the fastest working server and keeps LAN traffic local.
func DefaultState() State {
	return State{Auto: true, Routing: xrayconf.RoutingBypassLAN, SocksPort: 10808, HTTPPort: 10809}
}

// Use selects one server.
func (s *State) Use(srv *links.Server) {
	s.Auto, s.Key, s.Index, s.Name = false, srv.Key(), srv.Index, srv.Name
}

// UseAuto selects the balancer over all usable servers.
func (s *State) UseAuto() { s.Auto, s.Key, s.Index, s.Name = true, "", 0, "" }

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
	// XrayBin and AssetDir override the Xray used for validation. The
	// installer sets them to its staged, checksum-verified copies so the
	// bytes that run are the bytes that were verified.
	XrayBin, AssetDir string
}

// Installed reports whether sneakernet is installed on the target.
func (m *Manager) Installed() bool {
	_, err := os.Stat(m.T.Path(layout.XrayBin))
	return err == nil
}

// Configured reports whether an Xray config has been written yet. A fresh
// install without servers has none.
func (m *Manager) Configured() bool {
	_, err := os.Stat(m.T.Path(layout.ConfigFile))
	return err == nil
}

// Servers parses the installed server list. A missing list is empty.
func (m *Manager) Servers() ([]links.Server, []links.LineError, error) {
	b, err := os.ReadFile(m.T.Path(layout.ServersFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return links.ParseList(bytes.NewReader(b))
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

// SaveState writes the state without touching the Xray config.
func (m *Manager) SaveState(st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := m.T.EnsureDir(layout.EtcDir, 0o700); err != nil {
		return err
	}
	return m.T.WriteFile(layout.StateFile, append(b, '\n'), 0o600)
}

// resolve finds the chosen server by key (or by Index for states saved
// before keys existed) and refreshes its number and name.
func resolve(servers []links.Server, st State) (State, error) {
	if st.Auto {
		return st, nil
	}
	for i := range servers {
		s := &servers[i]
		if (st.Key != "" && s.Key() == st.Key) || (st.Key == "" && s.Index == st.Index) {
			st.Use(s)
			return st, nil
		}
	}
	return st, fmt.Errorf("the selected server %s is no longer in the list", st.Describe())
}

// Apply builds the config for st, checks it with Xray, and writes config and
// state. It does not restart the service; see Switch.
func (m *Manager) Apply(ctx context.Context, st State) (State, error) {
	servers, _, err := m.Servers()
	if err != nil {
		return st, err
	}
	if st, err = resolve(servers, st); err != nil {
		return st, err
	}
	cfg, err := xrayconf.Build(servers, xrayconf.Selection{Auto: st.Auto, Index: st.Index}, st.Options())
	if err != nil {
		return st, err
	}
	b, err := cfg.JSON()
	if err != nil {
		return st, err
	}
	if !m.SkipValidate {
		bin, assets := m.T.Path(layout.XrayBin), m.T.Path(layout.AssetDir)
		if m.XrayBin != "" {
			bin = m.XrayBin
		}
		if m.AssetDir != "" {
			assets = m.AssetDir
		}
		if err := xrayconf.Validate(ctx, bin, assets, b); err != nil {
			return st, err
		}
	}
	if err := m.T.EnsureDir(layout.EtcDir, 0o700); err != nil {
		return st, err
	}
	// The service user reads the config through its group; without that
	// user (no systemd) only root may read it.
	perm := os.FileMode(0o600)
	gid, gerr := service.GroupID(m.T, layout.ServiceUser)
	if gerr == nil {
		perm = 0o640
	}
	if err := m.T.WriteFile(layout.ConfigFile, b, perm); err != nil {
		return st, err
	}
	if gerr == nil {
		if err := service.ChownToGroup(m.T, layout.ConfigFile, gid); err != nil {
			return st, err
		}
	}
	return st, m.SaveState(st)
}

// Switch applies st and (re)starts the service so it takes effect. It also
// enables the service, which a fresh install without servers left disabled.
func (m *Manager) Switch(ctx context.Context, st State) (State, error) {
	st, err := m.Apply(ctx, st)
	if err != nil {
		return st, err
	}
	if m.Svc == nil {
		return st, errors.New("config written, but no supported service manager: restart Xray yourself")
	}
	if err := m.Svc.Enable(layout.UnitName); err != nil {
		return st, err
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

// AddPlan is what adding a block of pasted text would do.
type AddPlan struct {
	New        []links.Server // links not in the list yet
	Duplicates int            // links already in the list (or repeated)
	Errors     []links.LineError
}

// PlanAdd parses text (links, one per line, or a base64 subscription) and
// compares it with the installed list. It writes nothing.
func (m *Manager) PlanAdd(text string) (AddPlan, error) {
	var plan AddPlan
	existing, _, err := m.Servers()
	if err != nil {
		return plan, err
	}
	have := make(map[string]bool, len(existing))
	for _, s := range existing {
		have[s.Raw] = true
	}
	parsed, errs, err := links.ParseList(strings.NewReader(text))
	if err != nil {
		return plan, err
	}
	plan.Errors = errs
	for _, s := range parsed {
		if have[s.Raw] {
			plan.Duplicates++
			continue
		}
		have[s.Raw] = true
		plan.New = append(plan.New, s)
	}
	return plan, nil
}

// AddLinks appends the new links in text to the server list.
func (m *Manager) AddLinks(text string) (AddPlan, error) {
	plan, err := m.PlanAdd(text)
	if err != nil || len(plan.New) == 0 {
		return plan, err
	}
	path := m.T.Path(layout.ServersFile)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return plan, err
	}
	if !bytes.Contains(old, []byte("://")) && len(bytes.TrimSpace(old)) > 0 {
		// A base64 subscription: rewrite it as plain links first.
		old = plainList(m)
	}
	var b bytes.Buffer
	b.Write(old)
	if b.Len() > 0 && !bytes.HasSuffix(old, []byte("\n")) {
		b.WriteByte('\n')
	}
	for _, s := range plan.New {
		b.WriteString(s.Raw + "\n")
	}
	if err := m.T.EnsureDir(layout.EtcDir, 0o700); err != nil {
		return plan, err
	}
	return plan, m.T.WriteFile(layout.ServersFile, b.Bytes(), 0o600)
}

// RemoveServer deletes the server with the given key from the list.
func (m *Manager) RemoveServer(key string) (links.Server, error) {
	servers, _, err := m.Servers()
	if err != nil {
		return links.Server{}, err
	}
	var victim *links.Server
	for i := range servers {
		if servers[i].Key() == key {
			victim = &servers[i]
			break
		}
	}
	if victim == nil {
		return links.Server{}, errors.New("no such server")
	}
	path := m.T.Path(layout.ServersFile)
	old, err := os.ReadFile(path)
	if err != nil {
		return *victim, err
	}
	if !bytes.Contains(old, []byte("://")) {
		old = plainList(m)
	}
	// Keep comments and blank lines; drop only that link's line.
	var out []string
	removed := false
	for _, l := range strings.SplitAfter(string(old), "\n") {
		if !removed && strings.TrimSpace(strings.TrimPrefix(l, "\ufeff")) == victim.Raw {
			removed = true
			continue
		}
		out = append(out, l)
	}
	return *victim, m.T.WriteFile(layout.ServersFile, []byte(strings.Join(out, "")), 0o600)
}

// plainList renders the current servers as one link per line.
func plainList(m *Manager) []byte {
	servers, _, _ := m.Servers()
	var b bytes.Buffer
	for _, s := range servers {
		b.WriteString(s.Raw + "\n")
	}
	return b.Bytes()
}

// Remove deletes a server. If it was the active one, traffic moves to auto
// mode, or the service stops when no usable server is left. It returns the
// removed server and the state now in effect.
func (m *Manager) Remove(ctx context.Context, key string) (links.Server, State, error) {
	st, _ := m.State()
	removed, err := m.RemoveServer(key)
	if err != nil {
		return removed, st, err
	}
	switch {
	case !m.Configured():
		return removed, st, nil // the proxy is not set up yet
	case !st.Auto && st.Key != key:
		return removed, st, nil // another server is active; its config is unaffected
	case !st.Auto:
		st.UseAuto() // the active server is gone
	}
	// Auto mode's balancer lists every server, so re-apply in both cases.
	return m.refresh(ctx, removed, st)
}

// refresh re-applies st after the list changed, or stops the service when
// nothing usable is left.
func (m *Manager) refresh(ctx context.Context, removed links.Server, st State) (links.Server, State, error) {
	servers, _, err := m.Servers()
	if err != nil {
		return removed, st, err
	}
	for _, s := range servers {
		if s.Usable() {
			st, err = m.Switch(ctx, st)
			return removed, st, err
		}
	}
	if err := m.SaveState(st); err != nil {
		return removed, st, err
	}
	if m.Svc != nil {
		_ = m.Svc.Stop(layout.UnitName)
	}
	_ = os.Remove(m.T.Path(layout.ConfigFile))
	return removed, st, nil
}
