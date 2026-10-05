package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
	"github.com/AliSohani2082/sneakernet/internal/xraytest"
)

// installedRoot lays out the files the TUI needs in a temp root.
func installedRoot(t *testing.T) *manage.Manager {
	t.Helper()
	xray, assets := xraytest.Binary(t)
	tg := target.Dir(t.TempDir())
	copyFile := func(src, dst string, perm os.FileMode) {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		p := tg.Path(dst)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, perm); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(xray, layout.XrayBin, 0o755)
	for _, g := range layout.GeoFiles {
		copyFile(filepath.Join(assets, g), filepath.Join(layout.AssetDir, g), 0o644)
	}
	copyFile("../../test/fixtures/servers.txt", layout.ServersFile, 0o600)
	svc, err := service.For(tg, detect.Systemd) // restarts are no-ops on a non-running root
	if err != nil {
		t.Fatal(err)
	}
	m := &manage.Manager{T: tg, Svc: svc}
	if _, err := m.Apply(context.Background(), manage.DefaultState()); err != nil {
		t.Fatal(err)
	}
	return m
}

func press(t *testing.T, m *model, k tea.KeyPressMsg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(k)
	return cmd
}

func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func TestNavigateSwitchAndFilter(t *testing.T) {
	mgr := installedRoot(t)
	m, err := newModel(context.Background(), mgr)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if !strings.Contains(ansi.Strip(m.View().Content), "auto (fastest working server)") {
		t.Fatalf("initial view:\n%s", m.View().Content)
	}

	// Down twice and enter: server #3 becomes active and is saved.
	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	press(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	cmd := press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.busy != "switching" {
		t.Fatalf("busy = %q", m.busy)
	}
	m.Update(cmd()) // switchedMsg
	if m.state.Auto || m.state.Index != 3 {
		t.Fatalf("state after switch: %+v (msg %q)", m.state, m.msg)
	}
	if saved, _ := mgr.State(); saved.Index != 3 {
		t.Errorf("switch not saved: %+v", saved)
	}

	// The unusable plaintext server is refused with its reason.
	press(t, m, key('G'))
	m.busy = ""
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.msgErr || !strings.Contains(m.msg, "plaintext") {
		t.Errorf("unusable server: %q", m.msg)
	}

	// Filtering narrows the list; esc clears it.
	press(t, m, key('/'))
	for _, r := range "hyst" {
		press(t, m, key(r))
	}
	if len(m.visible) != 1 || m.servers[m.visible[0]].Name != "hysteria2" {
		t.Errorf("filter: %d visible", len(m.visible))
	}
	press(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(m.visible) != len(m.servers) {
		t.Errorf("filter not cleared: %d of %d", len(m.visible), len(m.servers))
	}

	// "a" goes back to auto.
	drainSwitch := press(t, m, key('a'))
	m.Update(drainSwitch())
	if !m.state.Auto {
		t.Errorf("auto: %+v", m.state)
	}

	if !strings.Contains(ansi.Strip(m.View().Content), "enter use") {
		t.Errorf("help line missing:\n%s", m.View().Content)
	}
	if cmd := press(t, m, key('q')); cmd == nil {
		t.Error("q should quit")
	}
}

func TestTestAllSortsWorkingFirst(t *testing.T) {
	bin, assets := xraytest.Binary(t)
	srv := xraytest.Start(t, bin, assets)
	mgr := installedRoot(t)
	// Replace the fixture list with the local server's links plus a dead one.
	list := strings.Join(append([]string{srv.DeadLink}, srv.Links...), "\n")
	if err := os.WriteFile(mgr.T.Path(layout.ServersFile), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := newModel(context.Background(), mgr)
	if err != nil {
		t.Fatal(err)
	}
	m.probeURL = srv.ProbeURL
	cmd := press(t, m, key('t'))
	if m.busy != "testing" || cmd == nil {
		t.Fatalf("t should start a test, busy=%q", m.busy)
	}
	m.Update(cmd())
	if m.servers[0].Name == "dead" || m.servers[len(m.servers)-1].Name != "dead" {
		t.Errorf("dead server should sort last: first=%q last=%q", m.servers[0].Name, m.servers[len(m.servers)-1].Name)
	}
	if !strings.Contains(m.msg, "5 of 6 servers work") {
		t.Errorf("msg = %q", m.msg)
	}
}
