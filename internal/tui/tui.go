// Package tui is the terminal UI. Its main screen is a search menu over the
// server list: typing filters and ranks servers live (name first, then
// other properties), and ctrl keys test the results, pick the fastest, add
// or remove servers and control the service. With an empty list it opens
// straight into the "add servers" screen.
package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/probe"
	"github.com/AliSohani2082/sneakernet/internal/search"
	"github.com/AliSohani2082/sneakernet/internal/service"
)

// Options tune how the UI looks. The zero value is "unicode, no color
// override".
type Options struct {
	// ASCII forces the console-safe symbol set. It is also chosen when
	// DetectASCII says the terminal needs it.
	ASCII bool
	// NoColor strips colors but keeps bold and faint. The caller decides
	// (NO_COLOR, --color) so the CLI and the TUI follow one rule.
	NoColor bool
	// NoFKeys is set on the bare Linux console, where F1-F5 do not arrive as
	// F-keys; the help bar then names ^g instead of F1.
	NoFKeys bool
}

// Run starts the UI and blocks until the user quits.
func Run(ctx context.Context, mgr *manage.Manager, opts Options) error {
	opts.ASCII = opts.ASCII || DetectASCII(os.Getenv)
	opts.NoFKeys = opts.NoFKeys || os.Getenv("TERM") == "linux"
	m, err := newModel(ctx, mgr, opts)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m, programOptions(ctx, opts)...).Run()
	return err
}

func programOptions(ctx context.Context, opts Options) []tea.ProgramOption {
	po := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.NoColor {
		po = append(po, tea.WithColorProfile(colorprofile.Ascii))
	}
	return po
}

type screen int

const (
	searchScreen screen = iota
	addScreen
	logsScreen
)

type sortMode int

const (
	byRelevance sortMode = iota
	bySpeed
)

func (s sortMode) String() string {
	if s == bySpeed {
		return "speed"
	}
	return "relevance"
}

// probeTimeout bounds each server test.
const probeTimeout = 10 * time.Second

// messageTTL is how long a status message stays; errors stay until a key.
const messageTTL = 5 * time.Second

type model struct {
	ctx      context.Context
	mgr      *manage.Manager
	probeURL string // "" = xrayconf.DefaultProbeURL

	servers []links.Server
	state   manage.State
	status  service.Status
	results map[string]probe.Result // by links.Server.Key

	screen   screen
	keys     keyMap
	help     help.Model
	spin     spinner.Model
	g        glyphs
	showHelp bool // the full help replaces the screen

	// search screen
	query  textinput.Model
	hits   []search.Hit
	order  sortMode
	cursor int
	offset int

	// add screen
	add      textarea.Model
	plan     *manage.AddPlan
	firstRun bool // the list was empty when the UI started

	// logs screen
	logs viewport.Model

	busy       string // what is running; actions wait for it
	cancel     context.CancelFunc
	cancelling bool // esc was pressed; waiting for the work to stop
	msg        string
	msgErr     bool
	msgID      int           // changes with every message, so a timer clears only its own
	msgTTL     time.Duration // 0 = messages stay until replaced
	confirm    *confirmation
	why        map[string]string // key -> why that action is unavailable now

	width, height int
}

// confirmation is a pending y/N question shown in the message line. yes
// builds the command only when confirmed, since starting work marks the UI busy.
type confirmation struct {
	prompt string
	yes    func() tea.Cmd
}

type (
	tickMsg     struct{}
	msgClearMsg struct{ id int }
	statusMsg   struct{ st service.Status }
	switchedMsg struct {
		st  manage.State
		err error
	}
	testedMsg struct {
		results map[string]probe.Result
		err     error
		useBest bool
	}
	checkMsg struct{ r probe.Result }
	logsMsg  struct{ text string }
	opMsg    struct {
		what string
		err  error
	}
	addedMsg struct {
		plan manage.AddPlan
		err  error
	}
	removedMsg struct {
		removed links.Server
		st      manage.State
		err     error
	}
)

func newModel(ctx context.Context, mgr *manage.Manager, opts Options) (*model, error) {
	g := pickGlyphs(opts.ASCII)
	m := &model{
		ctx: ctx, mgr: mgr, keys: newKeyMap(g, !opts.NoFKeys), help: newHelp(g), g: g,
		results: map[string]probe.Result{}, width: 100, height: 30,
		spin:   spinner.New(spinner.WithSpinner(g.spinner)),
		msgTTL: messageTTL, why: map[string]string{},
	}
	m.query = textinput.New()
	m.query.Prompt = g.cursor
	m.query.Placeholder = g.safe("search: name, type, host… e.g. berlin sec:reality")
	m.query.Focus()

	m.add = textarea.New()
	m.add.Placeholder = "vless://…\nvmess://…\ntrojan://…"
	m.add.ShowLineNumbers = false
	m.add.CharLimit, m.add.MaxHeight, m.add.MaxWidth = 0, 0, 0 // REALITY links are ~3000 chars

	m.logs = viewport.New()
	if err := m.reload(); err != nil {
		return nil, err
	}
	m.state, _ = mgr.State() // a missing state file means defaults
	if len(m.servers) == 0 {
		m.firstRun = true
		m.openAdd()
	}
	m.resize(m.width, m.height)
	m.syncKeys()
	return m, nil
}

// newHelp styles the help bar with ANSI colors only. The bubble's default
// greys are hex colors that fall back to near-black on a 16-color console.
func newHelp(g glyphs) help.Model {
	h := help.New()
	key := lipgloss.NewStyle().Bold(true)
	sep := lipgloss.NewStyle().Faint(true)
	h.Styles = help.Styles{
		Ellipsis: sep, ShortKey: key, ShortDesc: lipgloss.NewStyle(), ShortSeparator: sep,
		FullKey: key, FullDesc: lipgloss.NewStyle(), FullSeparator: sep,
	}
	h.ShortSeparator, h.Ellipsis = g.sep, g.ellipsis
	return h
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus, tick(), textinput.Blink)
}

func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// reload re-reads the server list and refreshes the results.
func (m *model) reload() error {
	servers, _, err := m.mgr.Servers()
	if err != nil {
		return err
	}
	m.servers = servers
	m.refresh()
	return nil
}

// refresh recomputes the search results for the current query and order.
func (m *model) refresh() {
	m.hits = search.Search(m.servers, m.query.Value())
	if m.order == bySpeed {
		rank := func(h search.Hit) (int, time.Duration) {
			r, ok := m.results[m.servers[h.Pos].Key()]
			switch {
			case ok && r.OK():
				return 0, r.Latency
			case !ok && m.servers[h.Pos].Usable():
				return 1, 0 // not tested yet
			default:
				return 2, 0
			}
		}
		sort.SliceStable(m.hits, func(a, b int) bool {
			ra, la := rank(m.hits[a])
			rb, lb := rank(m.hits[b])
			if ra != rb {
				return ra < rb
			}
			return la < lb
		})
	}
	m.clamp()
}

func (m *model) selected() *links.Server {
	if m.cursor < 0 || m.cursor >= len(m.hits) {
		return nil
	}
	return &m.servers[m.hits[m.cursor].Pos]
}

// usableHits are the servers in the current results that Xray can use.
func (m *model) usableHits() []links.Server {
	var out []links.Server
	for _, h := range m.hits {
		if s := m.servers[h.Pos]; s.Usable() {
			out = append(out, s)
		}
	}
	return out
}

func (m *model) setMsg(isErr bool, format string, a ...any) {
	m.msg, m.msgErr = m.g.safe(fmt.Sprintf(format, a...)), isErr
	m.msgID++
}

// clearMsg drops the message, if any.
func (m *model) clearMsg() {
	if m.msg != "" {
		m.msg, m.msgErr = "", false
		m.msgID++
	}
}

// endBusy marks the running work as finished and releases its context.
func (m *model) endBusy() {
	if m.cancel != nil {
		m.cancel()
	}
	m.busy, m.cancel, m.cancelling = "", nil, false
}

func (m *model) fetchStatus() tea.Msg {
	st, err := m.mgr.Status()
	if err != nil {
		st.State = "unknown: " + err.Error()
	}
	return statusMsg{st}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.msgID
	_, cmd := m.update(msg)
	m.syncKeys()
	// Notices fade after a while; errors stay until the next key press.
	if m.msgID != before && m.msg != "" && !m.msgErr && m.msgTTL > 0 {
		id := m.msgID
		cmd = tea.Batch(cmd, tea.Tick(m.msgTTL, func(time.Time) tea.Msg { return msgClearMsg{id} }))
	}
	return m, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.fetchStatus, tick())
	case statusMsg:
		m.status = msg.st
		return m, nil
	case msgClearMsg:
		if msg.id == m.msgID && m.busy == "" && m.confirm == nil {
			m.clearMsg()
		}
		return m, nil
	case spinner.TickMsg:
		if m.busy == "" {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case switchedMsg:
		return m.onSwitched(msg)
	case testedMsg:
		return m.onTested(msg)
	case checkMsg:
		cancelled := m.cancelling
		m.endBusy()
		if cancelled {
			m.setMsg(false, "connection check cancelled")
		} else if msg.r.OK() {
			m.setMsg(false, "connected through the proxy in %d ms", msg.r.Latency.Milliseconds())
		} else {
			m.setMsg(true, "no connection through the proxy: %v", msg.r.Err)
		}
		return m, nil
	case logsMsg:
		m.logs.SetContent(msg.text)
		m.logs.GotoBottom()
		return m, nil
	case opMsg:
		m.endBusy()
		if msg.err != nil {
			m.setMsg(true, "%s failed: %v", msg.what, msg.err)
		} else {
			m.setMsg(false, "%s done", msg.what)
		}
		return m, m.fetchStatus
	case addedMsg:
		return m.onAdded(msg)
	case removedMsg:
		return m.onRemoved(msg)
	case tea.PasteMsg:
		if m.showHelp {
			return m, nil
		}
		// Pasting links into the search box opens the add screen with them.
		if m.screen == searchScreen && strings.Contains(msg.Content, "://") {
			cmd := m.openAdd()
			m.add.InsertString(msg.Content)
			m.updatePlan()
			return m, cmd
		}
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m.forward(msg)
}

// forward hands other messages (cursor blink, paste, mouse) to the focused
// component.
func (m *model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.screen {
	case searchScreen:
		before := m.query.Value()
		m.query, cmd = m.query.Update(msg)
		if m.query.Value() != before {
			m.cursor, m.offset = 0, 0
			m.refresh()
		}
	case addScreen:
		m.add, cmd = m.add.Update(msg)
		m.updatePlan()
	case logsScreen:
		m.logs, cmd = m.logs.Update(msg)
	}
	return m, cmd
}

func (m *model) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(k, m.keys.Quit) {
		return m, tea.Quit
	}
	if m.showHelp { // any other key closes the help
		m.showHelp = false
		return m, nil
	}
	if m.msgErr && m.busy == "" && m.confirm == nil {
		m.clearMsg() // an error is read by now
	}
	if key.Matches(k, m.keys.Help) {
		m.showHelp = true
		return m, nil
	}
	if c := m.confirm; c != nil {
		m.confirm = nil
		switch k.String() {
		case "y", "Y":
			return m, c.yes()
		}
		m.setMsg(false, "cancelled")
		return m, nil
	}
	switch m.screen {
	case addScreen:
		return m.addKey(k)
	case logsScreen:
		return m.logsKey(k)
	}
	return m.searchKey(k)
}

func (m *model) searchKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Up):
		m.move(-1)
		return m, nil
	case key.Matches(k, m.keys.Down):
		m.move(1)
		return m, nil
	case key.Matches(k, m.keys.PageUp):
		m.move(-m.listHeight())
		return m, nil
	case key.Matches(k, m.keys.PageDown):
		m.move(m.listHeight())
		return m, nil
	case key.Matches(k, m.keys.Clear):
		if m.cancel != nil {
			return m.cancelBusy()
		}
		if m.query.Value() == "" {
			return m, tea.Quit
		}
		m.query.SetValue("")
		m.cursor, m.offset = 0, 0
		m.refresh()
		return m, nil
	case key.Matches(k, m.keys.Add):
		return m, m.openAdd()
	case key.Matches(k, m.keys.Logs):
		m.screen = logsScreen
		return m, m.fetchLogs
	case key.Matches(k, m.keys.Sort):
		m.order = 1 - m.order
		m.refresh()
		m.setMsg(false, "sorted by %s", m.order)
		return m, nil
	}
	if key.Matches(k, m.keys.actions()...) {
		if m.busy != "" {
			m.setMsg(true, "busy %s — wait a moment", m.busy)
			return m, nil
		}
		return m.action(k)
	}
	if why, ok := m.why[k.String()]; ok { // a known action that cannot run now
		m.setMsg(true, "%s", why)
		return m, nil
	}
	return m.forward(k) // everything else edits the search box
}

// cancelBusy asks the running work to stop. It stays marked busy until the
// work reports back, so nothing else starts on top of it.
func (m *model) cancelBusy() (tea.Model, tea.Cmd) {
	if !m.cancelling {
		m.cancelling = true
		m.cancel()
		m.setMsg(false, "cancelling…")
	}
	return m, nil
}

// syncKeys enables only the bindings that can do something now, so the help
// bar offers what works, and names the start/stop action truthfully.
func (m *model) syncKeys() {
	clear(m.why)
	gate := func(b *key.Binding, ok bool, why string) {
		b.SetEnabled(ok)
		if !ok && why != "" {
			for _, k := range b.Keys() {
				m.why[k] = why
			}
		}
	}
	searching := m.screen == searchScreen
	sel := m.selected()
	useWhy := ""
	if sel != nil && !sel.Usable() {
		useWhy = fmt.Sprintf("#%d cannot be used: %s", sel.Index, sel.Problem)
	}
	gate(&m.keys.Use, !searching || (sel != nil && sel.Usable()), useWhy)
	gate(&m.keys.Remove, !searching || sel != nil, "no server selected")
	nUsable := len(m.usableHits())
	gate(&m.keys.Test, !searching || nUsable > 0, "no usable servers in the results")
	gate(&m.keys.Best, !searching || nUsable > 0, "no usable servers in the results")
	gate(&m.keys.Auto, !searching || m.countUsable() > 0, "no usable servers: add some with ^n")
	if m.status.Active {
		m.keys.Power.SetHelp("^s", "stop")
	} else {
		m.keys.Power.SetHelp("^s", "start")
	}
	if m.cancel != nil {
		m.keys.Clear.SetHelp("esc", "cancel")
	} else {
		m.keys.Clear.SetHelp("esc", "clear/quit")
	}
}

func (m *model) countUsable() int {
	n := 0
	for i := range m.servers {
		if m.servers[i].Usable() {
			n++
		}
	}
	return n
}

func (m *model) action(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Use):
		s := m.selected()
		if s == nil {
			return m, nil
		}
		if !s.Usable() {
			m.setMsg(true, "#%d cannot be used: %s", s.Index, s.Problem)
			return m, nil
		}
		st := m.state
		st.Use(s)
		return m, m.doSwitch(st)
	case key.Matches(k, m.keys.Auto):
		st := m.state
		st.UseAuto()
		return m, m.doSwitch(st)
	case key.Matches(k, m.keys.Test):
		return m, m.testResults(false)
	case key.Matches(k, m.keys.Best):
		return m, m.useBest()
	case key.Matches(k, m.keys.Remove):
		s := m.selected()
		if s == nil {
			return m, nil
		}
		srv := *s
		m.confirm = &confirmation{
			prompt: fmt.Sprintf("Remove #%d %q from the list? (y/N)", srv.Index, srv.Name),
			yes:    func() tea.Cmd { return m.doRemove(srv) },
		}
		return m, nil
	case key.Matches(k, m.keys.Restart):
		return m, m.op("restart", func(svc service.Manager) error { return svc.Restart(layout.UnitName) })
	case key.Matches(k, m.keys.Power):
		if m.status.Active {
			return m, m.op("stop", func(svc service.Manager) error { return svc.Stop(layout.UnitName) })
		}
		return m, m.op("start", func(svc service.Manager) error { return svc.Start(layout.UnitName) })
	}
	return m, nil
}

// startBusy marks the UI busy. The returned context is cancelled by esc;
// pass it to the work so it stops early.
func (m *model) startBusy(what string) (context.Context, tea.Cmd) {
	m.endBusy()
	ctx, cancel := context.WithCancel(m.ctx)
	m.busy, m.cancel = what, cancel
	return ctx, m.spin.Tick
}

// startUncancellable is for work that must not be interrupted halfway, like
// rewriting the server list.
func (m *model) startUncancellable(what string) tea.Cmd {
	m.endBusy()
	m.busy = what
	return m.spin.Tick
}

func (m *model) doSwitch(st manage.State) tea.Cmd {
	m.setMsg(false, "switching to %s…", st.Describe())
	ctx, spin := m.startBusy("switching")
	return tea.Batch(spin, func() tea.Msg {
		st, err := m.mgr.Switch(ctx, st)
		return switchedMsg{st, err}
	})
}

func (m *model) onSwitched(msg switchedMsg) (tea.Model, tea.Cmd) {
	cancelled := m.cancelling
	m.endBusy()
	// A failed restart can follow a successful write; show what is saved.
	if saved, err := m.mgr.State(); err == nil {
		m.state = saved
	}
	if msg.err != nil {
		if cancelled {
			m.setMsg(false, "cancelled — the server was not changed")
		} else {
			m.setMsg(true, "switch failed: %v", msg.err)
		}
		return m, m.fetchStatus
	}
	m.state = msg.st
	m.setMsg(false, "now using %s — checking the connection…", msg.st.Describe())
	ctx, spin := m.startBusy("checking")
	return m, tea.Batch(m.fetchStatus, spin, m.check(ctx, 2*time.Second))
}

// testResults tests every usable server in the current results at once.
func (m *model) testResults(useBest bool) tea.Cmd {
	cands := m.usableHits()
	if len(cands) == 0 {
		m.setMsg(true, "no usable servers in the results")
		return nil
	}
	m.setMsg(false, "testing %d servers (up to %s)…", len(cands), probeTimeout)
	t, url := m.mgr.T, m.probeURL
	ctx, spin := m.startBusy("testing")
	return tea.Batch(spin, func() tea.Msg {
		byIndex, err := probe.All(ctx, t.Path(layout.XrayBin), t.Path(layout.AssetDir), cands, url, probeTimeout)
		results := make(map[string]probe.Result, len(byIndex))
		for _, s := range cands {
			if r, ok := byIndex[s.Index]; ok {
				results[s.Key()] = r
			}
		}
		return testedMsg{results: results, err: err, useBest: useBest}
	})
}

func (m *model) onTested(msg testedMsg) (tea.Model, tea.Cmd) {
	cancelled := m.cancelling
	m.endBusy()
	if cancelled { // half-finished results would only mislead
		m.setMsg(false, "cancelled — no servers were tested")
		return m, nil
	}
	if msg.err != nil {
		m.setMsg(true, "test failed: %v", msg.err)
		return m, nil
	}
	for k, r := range msg.results {
		m.results[k] = r
	}
	m.order = bySpeed
	m.refresh()
	m.cursor, m.offset = 0, 0
	best := m.fastest(msg.results)
	ok := 0
	for _, r := range msg.results {
		if r.OK() {
			ok++
		}
	}
	if best == nil {
		m.setMsg(true, "none of the %d tested servers work right now", len(msg.results))
		return m, nil
	}
	m.setMsg(false, "%d of %d work · fastest: %s (%d ms) · sorted by speed",
		ok, len(msg.results), best.Name, m.results[best.Key()].Latency.Milliseconds())
	if msg.useBest {
		st := m.state
		st.Use(best)
		return m, m.doSwitch(st)
	}
	return m, nil
}

// useBest switches to the fastest server in the current results, testing
// them first unless every one already has a result.
func (m *model) useBest() tea.Cmd {
	cands := m.usableHits()
	tested := map[string]probe.Result{}
	for _, s := range cands {
		r, ok := m.results[s.Key()]
		if !ok {
			return m.testResults(true)
		}
		tested[s.Key()] = r
	}
	best := m.fastest(tested)
	if best == nil {
		if len(cands) == 0 {
			m.setMsg(true, "no usable servers in the results")
		} else {
			m.setMsg(true, "none of the results worked when tested; ^t to test again")
		}
		return nil
	}
	st := m.state
	st.Use(best)
	return m.doSwitch(st)
}

// fastest is the working server with the lowest latency among results.
func (m *model) fastest(results map[string]probe.Result) *links.Server {
	var best *links.Server
	var bestLat time.Duration
	for i := range m.servers {
		s := &m.servers[i]
		r, ok := results[s.Key()]
		if ok && r.OK() && (best == nil || r.Latency < bestLat) {
			best, bestLat = s, r.Latency
		}
	}
	return best
}

func (m *model) check(ctx context.Context, delay time.Duration) tea.Cmd {
	port, url := m.state.SocksPort, m.probeURL
	return func() tea.Msg {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return checkMsg{probe.Result{Err: ctx.Err()}}
		}
		return checkMsg{probe.Through(ctx, fmt.Sprintf("127.0.0.1:%d", port), url, 15*time.Second)}
	}
}

func (m *model) op(what string, fn func(service.Manager) error) tea.Cmd {
	if m.mgr.Svc == nil {
		m.setMsg(true, "no supported service manager on this system")
		return nil
	}
	return tea.Batch(m.startUncancellable(what), func() tea.Msg { return opMsg{what, fn(m.mgr.Svc)} })
}

func (m *model) fetchLogs() tea.Msg {
	if m.mgr.Svc == nil {
		return logsMsg{"no service manager: no logs"}
	}
	text, err := m.mgr.Svc.Logs(layout.UnitName, 300)
	if err != nil {
		text += "\n" + err.Error()
	}
	return logsMsg{text}
}

func (m *model) doRemove(s links.Server) tea.Cmd {
	return tea.Batch(m.startUncancellable("removing"), func() tea.Msg {
		removed, st, err := m.mgr.Remove(m.ctx, s.Key())
		return removedMsg{removed, st, err}
	})
}

func (m *model) onRemoved(msg removedMsg) (tea.Model, tea.Cmd) {
	m.endBusy()
	if err := m.reload(); err != nil {
		m.setMsg(true, "%v", err)
		return m, nil
	}
	if msg.err != nil {
		m.setMsg(true, "remove failed: %v", msg.err)
		return m, nil
	}
	delete(m.results, msg.removed.Key())
	m.state = msg.st
	if m.mgr.Configured() {
		m.setMsg(false, "removed %q · using %s", msg.removed.Name, msg.st.Describe())
	} else {
		m.setMsg(false, "removed %q · no usable servers left, the proxy is off · ^n to add", msg.removed.Name)
	}
	return m, m.fetchStatus
}

// --- add screen ---

func (m *model) openAdd() tea.Cmd {
	m.screen = addScreen
	m.add.Reset()
	m.plan = nil
	m.query.Blur()
	return m.add.Focus()
}

func (m *model) updatePlan() {
	if strings.TrimSpace(m.add.Value()) == "" {
		m.plan = nil
		return
	}
	if plan, err := m.mgr.PlanAdd(m.add.Value()); err == nil {
		m.plan = &plan
	}
}

func (m *model) addKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Save):
		text := m.add.Value()
		if m.plan == nil || len(m.plan.New) == 0 {
			m.setMsg(true, "nothing new to add")
			return m, nil
		}
		return m, func() tea.Msg {
			plan, err := m.mgr.AddLinks(text)
			return addedMsg{plan, err}
		}
	case key.Matches(k, m.keys.Cancel):
		// Pasted links are long and hard to get back, so ask first.
		if m.plan != nil && len(m.plan.New) > 0 {
			n := len(m.plan.New)
			m.confirm = &confirmation{
				prompt: fmt.Sprintf("Discard %d pasted link(s)? (y/N)", n),
				yes:    m.leaveAdd,
			}
			return m, nil
		}
		return m, m.leaveAdd()
	}
	return m.forward(k)
}

// leaveAdd goes back to the search screen, or quits when the list is empty
// and there is nothing to go back to.
func (m *model) leaveAdd() tea.Cmd {
	if len(m.servers) == 0 {
		return tea.Quit
	}
	m.screen = searchScreen
	m.add.Blur()
	m.clearMsg()
	return m.query.Focus()
}

func (m *model) onAdded(msg addedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setMsg(true, "could not save: %v", msg.err)
		return m, nil
	}
	if err := m.reload(); err != nil {
		m.setMsg(true, "%v", err)
		return m, nil
	}
	m.screen = searchScreen
	m.add.Blur()
	m.query.SetValue("")
	m.refresh()
	// Put the cursor on the first new server.
	if len(msg.plan.New) > 0 {
		first := msg.plan.New[0].Raw
		for i, h := range m.hits {
			if m.servers[h.Pos].Raw == first {
				m.cursor = i
			}
		}
		m.clamp()
	}
	note := fmt.Sprintf("added %d server(s)", len(msg.plan.New))
	if msg.plan.Duplicates > 0 {
		note += fmt.Sprintf(", %d already listed", msg.plan.Duplicates)
	}
	if !m.mgr.Configured() {
		note += " · enter: use one · ^b: test and use the fastest · ^a: auto"
	}
	m.setMsg(false, "%s", note)
	m.firstRun = false
	return m, m.query.Focus()
}

// --- logs screen ---

func (m *model) logsKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Back):
		m.screen = searchScreen
		return m, m.query.Focus()
	case key.Matches(k, m.keys.Refresh):
		return m, m.fetchLogs
	}
	return m.forward(k)
}

// --- layout ---

func (m *model) resize(w, h int) {
	m.width, m.height = w, h
	m.query.SetWidth(max(w-30, 20))
	m.add.SetWidth(max(w-2, 20))
	m.add.SetHeight(max(h-9, 3))
	m.logs.SetWidth(w)
	m.logs.SetHeight(max(h-4, 3))
	m.help.SetWidth(w)
	m.clamp()
}

// listHeight is the number of result rows on the search screen.
func (m *model) listHeight() int { return max(m.height-9, 3) }

func (m *model) move(d int) {
	m.cursor += d
	m.clamp()
}

func (m *model) clamp() {
	if m.cursor >= len(m.hits) {
		m.cursor = len(m.hits) - 1
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
	if m.offset < 0 {
		m.offset = 0
	}
}
