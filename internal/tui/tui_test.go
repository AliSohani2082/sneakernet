package tui

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
	"github.com/AliSohani2082/sneakernet/internal/xraytest"
)

// installedRoot lays out an installation in a temp root with the given
// server list ("" = no list file at all).
func installedRoot(t *testing.T, servers string) *manage.Manager {
	t.Helper()
	xray, assets := xraytest.Binary(t)
	tg := target.Dir(t.TempDir())
	write := func(dst string, data []byte, perm os.FileMode) {
		p := tg.Path(dst)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, perm); err != nil {
			t.Fatal(err)
		}
	}
	copyFile := func(src, dst string, perm os.FileMode) {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		write(dst, data, perm)
	}
	copyFile(xray, layout.XrayBin, 0o755)
	for _, g := range layout.GeoFiles {
		copyFile(filepath.Join(assets, g), filepath.Join(layout.AssetDir, g), 0o644)
	}
	if servers != "" {
		write(layout.ServersFile, []byte(servers), 0o600)
	}
	svc, err := service.For(tg, detect.Systemd) // start/restart are no-ops on a non-running root
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Install(layout.UnitName, service.XrayUnit()); err != nil {
		t.Fatal(err)
	}
	mgr := &manage.Manager{T: tg, Svc: svc}
	// Use ports nothing listens on, so connection checks after a switch fail
	// fast instead of reaching a proxy that runs on the test machine.
	st := manage.DefaultState()
	st.SocksPort, st.HTTPPort = unusedPort(t), unusedPort(t)
	if err := mgr.SaveState(st); err != nil {
		t.Fatal(err)
	}
	return mgr
}

func unusedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../test/fixtures/servers.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newTestModel(t *testing.T, mgr *manage.Manager) *model {
	t.Helper()
	return newSizedModel(t, mgr, Options{}, 120, 30)
}

// newSizedModel builds a model of a given size. Messages do not fade: the
// test helpers would otherwise wait for the timers.
func newSizedModel(t *testing.T, mgr *manage.Manager, opts Options, w, h int) *model {
	t.Helper()
	m, err := newModel(context.Background(), mgr, opts)
	if err != nil {
		t.Fatal(err)
	}
	m.msgTTL = 0
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// do sends a message and runs the resulting commands like the program loop
// would, feeding back only this package's messages (timers are skipped).
func do(t *testing.T, m *model, msg tea.Msg) {
	t.Helper()
	_, cmd := m.Update(msg)
	run(t, m, cmd)
}

func run(t *testing.T, m *model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("command did not finish")
	}
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(t, m, c)
		}
	case switchedMsg, testedMsg, checkMsg, addedMsg, removedMsg, opMsg, logsMsg, statusMsg:
		_, next := m.Update(msg)
		run(t, m, next)
	}
}

// typeText types into the focused input. Typing only returns cursor-blink
// timers, so their commands are not run.
func typeText(t *testing.T, m *model, s string) {
	t.Helper()
	for _, r := range s {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func view(m *model) string { return ansi.Strip(m.View().Content) }

func names(m *model) []string {
	var out []string
	for _, h := range m.hits {
		out = append(out, m.servers[h.Pos].Name)
	}
	return out
}

func TestEmptyListAsksForServers(t *testing.T) {
	for name, list := range map[string]string{"missing": "", "only comments": "# nothing yet\n\n"} {
		t.Run(name, func(t *testing.T) {
			mgr := installedRoot(t, list)
			m := newTestModel(t, mgr)
			if m.screen != addScreen || !strings.Contains(view(m), "There are no servers yet") {
				t.Fatalf("want the add screen first:\n%s", view(m))
			}
			do(t, m, tea.PasteMsg{Content: fixture(t)})
			if m.plan == nil || len(m.plan.New) != 13 || !strings.Contains(view(m), "13 new") {
				t.Fatalf("preview: %+v\n%s", m.plan, view(m))
			}
			do(t, m, ctrl('s'))
			if m.screen != searchScreen || len(m.servers) != 13 {
				t.Fatalf("after save: screen %d, %d servers, msg %q", m.screen, len(m.servers), m.msg)
			}
			if !strings.Contains(m.msg, "added 13") || !strings.Contains(m.msg, "enter: use one") {
				t.Errorf("msg %q", m.msg)
			}
			saved, _, _ := mgr.Servers()
			if len(saved) != 13 {
				t.Errorf("servers file has %d servers", len(saved))
			}
		})
	}
}

func TestLiveSearchRanksNameFirst(t *testing.T) {
	m := newTestModel(t, installedRoot(t, fixture(t)))
	if len(m.hits) != 13 {
		t.Fatalf("all servers before typing: %d", len(m.hits))
	}
	typeText(t, m, "tls")
	got := names(m)
	// Names containing "tls" first, then servers whose security is tls.
	nameMatch := map[string]bool{"vless ws tls": true, "vless grpc tls": true,
		"vless httpupgrade tls": true, "vmess ws tls": true, "trojan tls": true}
	for i, n := range got {
		if (i < len(nameMatch)) != nameMatch[n] {
			t.Errorf("rank %d is %q; name matches should come first: %q", i, n, got)
		}
	}
	// hysteria2 by security, vless raw reality by its xtls-rprx-vision flow.
	if len(got) != 7 || !strings.Contains(view(m), "·sec") || !strings.Contains(view(m), "·flow") {
		t.Errorf("property matches missing: %q", got)
	}
	if !strings.Contains(view(m), "7 of 13") {
		t.Errorf("count missing:\n%s", view(m))
	}

	do(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.query.Value() != "" || len(m.hits) != 13 {
		t.Errorf("esc should clear the search")
	}
	typeText(t, m, "sec:reality")
	if len(m.hits) != 2 {
		t.Errorf("qualifier: %q", names(m))
	}
}

func TestUseAndRemove(t *testing.T) {
	mgr := installedRoot(t, fixture(t))
	m := newTestModel(t, mgr)
	typeText(t, m, "grpc")
	do(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state.Auto || m.state.Name != "vless grpc tls" || !mgr.Configured() {
		t.Fatalf("after enter: %+v (msg %q)", m.state, m.msg)
	}
	if !strings.Contains(view(m), `Using #4 "vless grpc tls"`) {
		t.Errorf("header:\n%s", view(m))
	}

	// Removing the active server asks first, then falls back to auto.
	do(t, m, ctrl('x'))
	if m.confirm == nil || !strings.Contains(view(m), `Remove #4 "vless grpc tls"`) {
		t.Fatalf("no confirmation:\n%s", view(m))
	}
	do(t, m, tea.KeyPressMsg{Code: 'n', Text: "n"})
	if len(m.servers) != 13 {
		t.Fatal("answering n removed the server")
	}
	do(t, m, ctrl('x'))
	do(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(m.servers) != 12 || !m.state.Auto {
		t.Fatalf("after remove: %d servers, state %+v, msg %q", len(m.servers), m.state, m.msg)
	}
	if saved, err := mgr.State(); err != nil || !saved.Auto {
		t.Errorf("saved state %+v %v", saved, err)
	}
}

func TestTestResultsAndUseFastest(t *testing.T) {
	bin, assets := xraytest.Binary(t)
	srv := xraytest.Start(t, bin, assets)
	list := strings.Join(append(append([]string{srv.DeadLink}, srv.Links...),
		strings.Split(strings.TrimSpace(fixture(t)), "\n")...), "\n")
	mgr := installedRoot(t, list)
	m := newTestModel(t, mgr)
	m.probeURL = srv.ProbeURL

	// ^t tests only what the search shows: the five local servers.
	typeText(t, m, "local")
	if len(m.hits) != 5 {
		t.Fatalf("hits for 'local': %q", names(m))
	}
	do(t, m, ctrl('t'))
	if len(m.results) != 5 || m.order != bySpeed || !strings.Contains(m.msg, "5 of 5 work") {
		t.Fatalf("tested %d servers, order %v, msg %q", len(m.results), m.order, m.msg)
	}

	// Widen to every server on 127.0.0.1: the dead one is untested, so ^b
	// tests the results first, then switches to the fastest.
	do(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	typeText(t, m, "host:127.0.0.1")
	if len(m.hits) != 6 {
		t.Fatalf("hits: %q", names(m))
	}
	do(t, m, ctrl('b'))
	if m.state.Auto || !strings.HasPrefix(m.state.Name, "local ") {
		t.Fatalf("fastest pick: %+v (msg %q)", m.state, m.msg)
	}
	last := m.servers[m.hits[len(m.hits)-1].Pos]
	if r, ok := m.results[last.Key()]; last.Name != "dead" || !ok || r.OK() {
		t.Errorf("the dead server should sort last as failed: %q", names(m))
	}
}
