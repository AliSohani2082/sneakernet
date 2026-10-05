// Package tui is the terminal UI: pick a server, test them all, control the
// service and read its logs.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/probe"
	"github.com/AliSohani2082/sneakernet/internal/service"
)

// Run starts the UI and blocks until the user quits.
func Run(ctx context.Context, mgr *manage.Manager) error {
	m, err := newModel(ctx, mgr)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	keyStyle    = lipgloss.NewStyle().Bold(true)
)

type model struct {
	ctx      context.Context
	mgr      *manage.Manager
	probeURL string // "" = xrayconf.DefaultProbeURL
	servers  []links.Server
	state    manage.State
	status   service.Status
	results  map[int]probe.Result

	visible   []int // indexes into servers after filtering
	cursor    int   // position in visible
	offset    int   // first visible row shown
	filter    textinput.Model
	filtering bool

	busy     string // what is running now
	msg      string
	msgErr   bool
	showLogs bool
	logs     string

	width, height int
}

type (
	tickMsg     struct{}
	statusMsg   struct{ st service.Status }
	switchedMsg struct {
		st  manage.State
		err error
	}
	probeAllMsg struct {
		results map[int]probe.Result
		err     error
	}
	checkMsg struct{ r probe.Result }
	logsMsg  struct{ text string }
	opMsg    struct {
		what string
		err  error
	}
)

func newModel(ctx context.Context, mgr *manage.Manager) (*model, error) {
	servers, _, err := mgr.Servers()
	if err != nil {
		return nil, err
	}
	st, err := mgr.State()
	if err != nil {
		return nil, err
	}
	fi := textinput.New()
	fi.Prompt = "/ "
	fi.Placeholder = "filter by name or type"
	m := &model{ctx: ctx, mgr: mgr, servers: servers, state: st, filter: fi,
		results: map[int]probe.Result{}, width: 100, height: 30}
	m.refilter()
	// Start on the active server.
	for i, idx := range m.visible {
		if !st.Auto && m.servers[idx].Index == st.Index {
			m.cursor = i
		}
	}
	return m, nil
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.fetchStatus, tick()) }

func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) fetchStatus() tea.Msg {
	st, err := m.mgr.Status()
	if err != nil {
		st.State = "unknown: " + err.Error()
	}
	return statusMsg{st}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
	case tickMsg:
		return m, tea.Batch(m.fetchStatus, tick())
	case statusMsg:
		m.status = msg.st
	case switchedMsg:
		m.busy = ""
		// A failed restart can follow a successful write; show what is saved.
		if saved, err := m.mgr.State(); err == nil {
			m.state = saved
		}
		if msg.err != nil {
			m.setMsg(true, "switch failed: %v", msg.err)
			return m, nil
		}
		m.state = msg.st
		m.setMsg(false, "now using %s — checking the connection…", msg.st.Describe())
		m.busy = "checking"
		return m, tea.Batch(m.fetchStatus, m.check(2*time.Second))
	case probeAllMsg:
		m.busy = ""
		if msg.err != nil {
			m.setMsg(true, "test failed: %v", msg.err)
			return m, nil
		}
		m.results = msg.results
		ok := 0
		for _, r := range msg.results {
			if r.OK() {
				ok++
			}
		}
		m.setMsg(ok == 0, "%d of %d servers work right now (sorted fastest first)", ok, len(msg.results))
		m.sortByResults()
	case checkMsg:
		m.busy = ""
		if msg.r.OK() {
			m.setMsg(false, "connected through the proxy in %d ms", msg.r.Latency.Milliseconds())
		} else {
			m.setMsg(true, "no connection through the proxy: %v", msg.r.Err)
		}
	case logsMsg:
		m.logs = msg.text
	case opMsg:
		m.busy = ""
		if msg.err != nil {
			m.setMsg(true, "%s failed: %v", msg.what, msg.err)
		} else {
			m.setMsg(false, "%s done", msg.what)
		}
		return m, m.fetchStatus
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *model) setMsg(isErr bool, format string, a ...any) {
	m.msg, m.msgErr = fmt.Sprintf(format, a...), isErr
}

func (m *model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.filtering {
		switch key {
		case "enter":
			m.filtering = false
			m.filter.Blur()
			return m, nil
		case "esc":
			m.filtering = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.refilter()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		m.refilter()
		return m, cmd
	}
	if m.showLogs {
		switch key {
		case "l", "esc", "q":
			m.showLogs = false
		case "r":
			return m, m.fetchLogs
		}
		return m, nil
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.listHeight())
	case "pgdown":
		m.move(m.listHeight())
	case "home", "g":
		m.move(-len(m.visible))
	case "end", "G":
		m.move(len(m.visible))
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "l":
		m.showLogs = true
		return m, m.fetchLogs
	}
	if m.busy != "" {
		switch key {
		case "enter", "a", "t", "c", "r", "s":
			m.setMsg(true, "busy %s — wait a moment", m.busy)
		}
		return m, nil
	}
	switch key {
	case "enter":
		if len(m.visible) == 0 {
			return m, nil
		}
		s := m.servers[m.visible[m.cursor]]
		if !s.Usable() {
			m.setMsg(true, "#%d cannot be used: %s", s.Index, s.Problem)
			return m, nil
		}
		st := m.state
		st.Auto, st.Index = false, s.Index
		return m, m.doSwitch(st)
	case "a":
		st := m.state
		st.Auto, st.Index = true, 0
		return m, m.doSwitch(st)
	case "t":
		m.busy = "testing"
		m.setMsg(false, "testing every server — this takes up to ~15 seconds…")
		return m, m.probeAll
	case "c":
		m.busy = "checking"
		m.setMsg(false, "checking the connection…")
		return m, m.check(0)
	case "r":
		m.busy = "restarting"
		return m, m.op("restart", func() error { return m.mgr.Svc.Restart(layout.UnitName) })
	case "s":
		if m.status.Active {
			m.busy = "stopping"
			return m, m.op("stop", func() error { return m.mgr.Svc.Stop(layout.UnitName) })
		}
		m.busy = "starting"
		return m, m.op("start", func() error { return m.mgr.Svc.Start(layout.UnitName) })
	}
	return m, nil
}

func (m *model) doSwitch(st manage.State) tea.Cmd {
	m.busy = "switching"
	m.setMsg(false, "switching to %s…", st.Describe())
	return func() tea.Msg {
		st, err := m.mgr.Switch(m.ctx, st)
		return switchedMsg{st, err}
	}
}

func (m *model) probeAll() tea.Msg {
	t := m.mgr.T
	r, err := probe.All(m.ctx, t.Path(layout.XrayBin), t.Path(layout.AssetDir), m.servers, m.probeURL, 10*time.Second)
	return probeAllMsg{r, err}
}

func (m *model) check(delay time.Duration) tea.Cmd {
	port := m.state.SocksPort
	return func() tea.Msg {
		time.Sleep(delay)
		return checkMsg{probe.Through(m.ctx, fmt.Sprintf("127.0.0.1:%d", port), m.probeURL, 15*time.Second)}
	}
}

func (m *model) op(what string, fn func() error) tea.Cmd {
	if m.mgr.Svc == nil {
		m.busy = ""
		m.setMsg(true, "no supported service manager on this system")
		return nil
	}
	return func() tea.Msg { return opMsg{what, fn()} }
}

func (m *model) fetchLogs() tea.Msg {
	if m.mgr.Svc == nil {
		return logsMsg{"no service manager: no logs"}
	}
	text, err := m.mgr.Svc.Logs(layout.UnitName, 200)
	if err != nil {
		text += "\n" + err.Error()
	}
	return logsMsg{text}
}

func (m *model) refilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = m.visible[:0]
	for i, s := range m.servers {
		if q == "" || strings.Contains(strings.ToLower(s.Name+" "+s.Kind()), q) {
			m.visible = append(m.visible, i)
		}
	}
	m.clampScroll()
}

// sortByResults orders the list: working servers by latency, then the rest.
func (m *model) sortByResults() {
	rank := func(i int) (int, time.Duration) {
		r, ok := m.results[m.servers[i].Index]
		switch {
		case ok && r.OK():
			return 0, r.Latency
		case m.servers[i].Usable():
			return 1, 0
		default:
			return 2, 0
		}
	}
	sort.SliceStable(m.servers, func(a, b int) bool {
		ra, la := rank(a)
		rb, lb := rank(b)
		if ra != rb {
			return ra < rb
		}
		return la < lb
	})
	m.cursor, m.offset = 0, 0
	m.refilter()
}

func (m *model) move(d int) {
	m.cursor += d
	m.clampScroll()
}

func (m *model) listHeight() int {
	h := m.height - 8
	if h < 3 {
		h = 3
	}
	return h
}

func (m *model) clampScroll() {
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
}

func (m *model) View() tea.View {
	var b strings.Builder
	w := m.width
	state := errStyle.Render(m.status.State)
	if m.status.Active {
		state = okStyle.Render(m.status.State)
	}
	if m.status.State == "" {
		state = dimStyle.Render("…")
	}
	fmt.Fprintf(&b, "%s  %s  %s\n", titleStyle.Render("Sneakernet"), state,
		dimStyle.Render(fmt.Sprintf("SOCKS 127.0.0.1:%d · HTTP 127.0.0.1:%d", m.state.SocksPort, m.state.HTTPPort)))
	routing := m.state.Routing
	if m.state.Region != "" {
		routing += " (" + m.state.Region + ")"
	}
	fmt.Fprintf(&b, "Using %s · routing %s\n", titleStyle.Render(m.state.Describe()), routing)
	b.WriteString(dimStyle.Render(strings.Repeat("─", max(w, 10))) + "\n")

	if m.showLogs {
		lines := strings.Split(strings.TrimRight(m.logs, "\n"), "\n")
		h := m.height - 6
		if len(lines) > h && h > 0 {
			lines = lines[len(lines)-h:]
		}
		for _, l := range lines {
			b.WriteString(ansi.Truncate(l, w, "…") + "\n")
		}
		b.WriteString(dimStyle.Render(strings.Repeat("─", max(w, 10))) + "\n")
		b.WriteString(help("r", "refresh", "l/esc", "back", "ctrl+c", "quit"))
		return m.view(b.String())
	}

	nameW := w - 4 - 6 - 24 - 22
	if nameW < 16 {
		nameW = 16
	}
	h := m.listHeight()
	for row := 0; row < h; row++ {
		i := m.offset + row
		if i >= len(m.visible) {
			b.WriteString("\n")
			continue
		}
		s := m.servers[m.visible[i]]
		mark := " "
		if !m.state.Auto && s.Index == m.state.Index {
			mark = okStyle.Render("●")
		}
		note := ""
		if r, ok := m.results[s.Index]; ok {
			if r.OK() {
				note = okStyle.Render(fmt.Sprintf("%d ms", r.Latency.Milliseconds()))
			} else {
				note = errStyle.Render(ansi.Truncate(r.Err.Error(), 22, "…"))
			}
		} else if !s.Usable() {
			note = ansi.Truncate(s.Problem, 22, "…")
		}
		line := fmt.Sprintf("%s %4d  %s  %s  %s", mark, s.Index, pad(s.Name, nameW), pad(s.Kind(), 24), note)
		switch {
		case i == m.cursor:
			line = cursorStyle.Render(ansi.Strip(line))
		case !s.Usable():
			line = dimStyle.Render(ansi.Strip(line))
		}
		b.WriteString(ansi.Truncate(line, w, "") + "\n")
	}
	b.WriteString(dimStyle.Render(strings.Repeat("─", max(w, 10))) + "\n")

	switch {
	case m.filtering || m.filter.Value() != "":
		b.WriteString(m.filter.View() + dimStyle.Render(fmt.Sprintf("  %d shown", len(m.visible))) + "\n")
	case m.msg != "":
		style := okStyle
		if m.msgErr {
			style = errStyle
		}
		b.WriteString(style.Render(ansi.Truncate(m.msg, w, "…")) + "\n")
	default:
		b.WriteString("\n")
	}
	b.WriteString(help("enter", "use", "a", "auto", "t", "test all", "c", "check", "r", "restart",
		"s", "stop/start", "l", "logs", "/", "filter", "q", "quit"))
	return m.view(b.String())
}

func (m *model) view(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func help(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+dimStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, dimStyle.Render(" · "))
}

// pad fits s into exactly n terminal cells (emoji flags are two cells wide).
func pad(s string, n int) string {
	s = ansi.Truncate(s, n, "…")
	if w := ansi.StringWidth(s); w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}
