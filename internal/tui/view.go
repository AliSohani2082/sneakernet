package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/search"
)

// kindW fits "hysteria2 hysteria/tls ·sec".
const kindW = 30

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	matchStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Underline(true)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

func (m *model) View() tea.View {
	var body string
	switch m.screen {
	case addScreen:
		body = m.addView()
	case logsScreen:
		body = m.logsView()
	default:
		body = m.searchView()
	}
	if m.showHelp {
		body = m.helpView()
	}
	v := tea.NewView(m.g.safe(body))
	v.AltScreen = true
	return v
}

func (m *model) rule() string { return dimStyle.Render(strings.Repeat(m.g.rule, max(m.width, 10))) }

func (m *model) header() string {
	state := dimStyle.Render(m.g.ellipsis)
	switch {
	case m.status.Active:
		state = okStyle.Render(m.g.active + " " + m.status.State)
	case m.status.State != "":
		state = errStyle.Render(m.g.inactive + " " + m.status.State)
	}
	using := titleStyle.Render(m.state.Describe())
	if !m.mgr.Configured() {
		using = warnStyle.Render("no server chosen yet")
	}
	routing := m.state.Routing
	if m.state.Region != "" {
		routing += " (" + m.state.Region + ")"
	}
	return fmt.Sprintf("%s  %s  %s\nUsing %s %s\n",
		titleStyle.Render("Sneakernet"), state,
		dimStyle.Render(fmt.Sprintf("SOCKS 127.0.0.1:%d %s HTTP 127.0.0.1:%d", m.state.SocksPort, m.g.dot, m.state.HTTPPort)),
		using, dimStyle.Render(m.g.dot+" routing "+routing))
}

func (m *model) searchView() string {
	var b strings.Builder
	w := m.width
	b.WriteString(m.header())

	count := fmt.Sprintf("%d of %d %s by %s", len(m.hits), len(m.servers), m.g.dot, m.order)
	box := m.query.View()
	gap := max(w-ansi.StringWidth(box)-ansi.StringWidth(count)-1, 1)
	b.WriteString(box + strings.Repeat(" ", gap) + dimStyle.Render(count) + "\n")
	b.WriteString(m.rule() + "\n")

	h := m.listHeight()
	nameW := max(w-4-5-2-kindW-2-16, 16)
	for row := 0; row < h; row++ {
		i := m.offset + row
		if i >= len(m.hits) {
			if i == 0 && row == 0 {
				b.WriteString(dimStyle.Render("  no servers match - esc clears the search, ^n adds servers") + "\n")
				continue
			}
			b.WriteString("\n")
			continue
		}
		b.WriteString(ansi.Truncate(m.row(m.hits[i], i == m.cursor, nameW), w, "") + "\n")
	}
	b.WriteString(m.rule() + "\n")
	b.WriteString(ansi.Truncate(m.details(), w, m.g.ellipsis) + "\n")
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(m.help.ShortHelpView(m.keys.searchHelp()))
	return b.String()
}

func (m *model) row(h search.Hit, selected bool, nameW int) string {
	s := &m.servers[h.Pos]
	cur := "  "
	if selected {
		cur = cursorStyle.Render(m.g.cursor)
	}
	active := " "
	if m.mgr.Configured() && !m.state.Auto && s.Key() == m.state.Key {
		active = okStyle.Render(m.g.active)
	}
	base := lipgloss.NewStyle()
	if selected {
		base = base.Bold(true)
	}
	if !s.Usable() {
		base = base.Faint(true)
	}
	name, matches := s.Name, h.NameRunes
	if m.g.ascii {
		name, matches = asciiName(name, matches)
	}
	name = m.pad(lipgloss.StyleRunes(name, matches, matchStyle, base), nameW)
	kind := s.Kind()
	if h.Field != "" {
		kind += " " + m.g.dot + h.Field
	}
	return fmt.Sprintf("%s%s%4d  %s  %s  %s", cur, active, s.Index, name,
		base.Faint(true).Render(m.pad(kind, kindW)), m.note(s))
}

// note is the right-hand column: test result, or why a server is unusable.
func (m *model) note(s *links.Server) string {
	if r, ok := m.results[s.Key()]; ok {
		if r.OK() {
			return okStyle.Render(fmt.Sprintf("%5d ms", r.Latency.Milliseconds()))
		}
		return errStyle.Render(ansi.Truncate("failed: "+r.Err.Error(), 16, m.g.ellipsis))
	}
	if !s.Usable() {
		return dimStyle.Render(ansi.Truncate(s.Problem, 16, m.g.ellipsis))
	}
	return ""
}

// details describes the selected server on one line.
func (m *model) details() string {
	s := m.selected()
	if s == nil {
		return ""
	}
	parts := []string{net.JoinHostPort(s.Address, strconv.Itoa(s.Port))}
	if s.Security.SNI != "" {
		parts = append(parts, "sni "+s.Security.SNI)
	}
	if p := s.Transport.Path + s.Transport.ServiceName; p != "" {
		parts = append(parts, "path "+p)
	}
	if s.Flow != "" {
		parts = append(parts, s.Flow)
	}
	line := dimStyle.Render(strings.Join(parts, " "+m.g.dot+" "))
	switch {
	case !s.Usable():
		line += "  " + errStyle.Render(s.Problem)
	case len(s.Warnings) > 0:
		line += "  " + warnStyle.Render(s.Warnings[0])
	}
	return line
}

func (m *model) statusLine() string {
	w := m.width
	switch {
	case m.confirm != nil:
		return warnStyle.Render(ansi.Truncate(m.confirm.prompt, w, m.g.ellipsis))
	case m.busy != "":
		msg := m.msg
		if m.cancel != nil && !m.cancelling {
			msg += "   esc cancel"
		}
		return m.spin.View() + " " + ansi.Truncate(msg, w-2, m.g.ellipsis)
	case m.msg != "":
		style := okStyle
		if m.msgErr {
			style = errStyle
		}
		return style.Render(ansi.Truncate(m.msg, w, m.g.ellipsis))
	}
	return ""
}

func (m *model) addView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Sneakernet - add servers") + "\n")
	intro := "Paste share links below, one per line (vless://, vmess://, trojan://, ss://, hysteria2://) or a base64 subscription."
	if m.firstRun {
		intro = warnStyle.Render("There are no servers yet. ") + intro
	}
	b.WriteString(ansi.Wrap(intro, m.width, "") + "\n\n")
	b.WriteString(m.add.View() + "\n")
	b.WriteString(m.rule() + "\n")
	b.WriteString(m.planLine() + "\n")
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(m.help.ShortHelpView(m.keys.addHelp()))
	return b.String()
}

// planLine previews what saving would add.
func (m *model) planLine() string {
	p := m.plan
	if p == nil {
		return dimStyle.Render("nothing pasted yet")
	}
	usable := 0
	for _, s := range p.New {
		if s.Usable() {
			usable++
		}
	}
	parts := []string{okStyle.Render(fmt.Sprintf("%d new", len(p.New)))}
	if n := len(p.New) - usable; n > 0 {
		parts = append(parts, warnStyle.Render(fmt.Sprintf("%d of them unusable", n)))
	}
	if p.Duplicates > 0 {
		parts = append(parts, dimStyle.Render(fmt.Sprintf("%d already listed", p.Duplicates)))
	}
	if len(p.Errors) > 0 {
		parts = append(parts, errStyle.Render(fmt.Sprintf("%d unreadable (line %d: %v)",
			len(p.Errors), p.Errors[0].Line, p.Errors[0].Err)))
	}
	return ansi.Truncate(strings.Join(parts, " "+m.g.dot+" "), m.width, m.g.ellipsis)
}

func (m *model) logsView() string {
	return m.header() + m.rule() + "\n" + m.logs.View() + "\n" + m.help.ShortHelpView(m.keys.logsHelp())
}

// pad fits s into exactly n terminal cells (emoji flags are two cells wide).
func (m *model) pad(s string, n int) string {
	s = ansi.Truncate(s, n, m.g.ellipsis)
	if w := ansi.StringWidth(s); w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}

// bindingLabel is "^t / F5": the shown key plus its ctrl and F-key aliases.
func bindingLabel(b key.Binding) string {
	label := b.Help().Key
	seen := map[string]bool{strings.ToLower(label): true}
	parts := []string{label}
	for _, k := range b.Keys() {
		alias := ""
		switch {
		case strings.HasPrefix(k, "ctrl+"):
			alias = "^" + strings.TrimPrefix(k, "ctrl+")
		case len(k) >= 2 && k[0] == 'f' && k[1] >= '0' && k[1] <= '9':
			alias = strings.ToUpper(k)
		}
		if alias != "" && !seen[strings.ToLower(alias)] {
			seen[strings.ToLower(alias)] = true
			parts = append(parts, alias)
		}
	}
	return strings.Join(parts, " / ")
}

// helpView is the full help: every binding grouped, the search syntax and the
// command-line equivalents. Any key closes it.
func (m *model) helpView() string {
	power := m.keys.Power
	power.SetHelp("^s", "start/stop") // the bar names the action; here it is both
	groups := []struct {
		title string
		keys  []key.Binding
	}{
		{"Move", []key.Binding{m.keys.Up, m.keys.Down, m.keys.PageUp, m.keys.PageDown, m.keys.Clear}},
		{"Servers", []key.Binding{m.keys.Use, m.keys.Test, m.keys.Best, m.keys.Auto, m.keys.Sort}},
		{"List and screens", []key.Binding{m.keys.Add, m.keys.Remove, m.keys.Logs}},
		{"Service and app", []key.Binding{m.keys.Restart, power, m.keys.Help, m.keys.Quit}},
	}
	const col = 38
	block := func(gs ...int) []string {
		var lines []string
		for n, gi := range gs {
			if n > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, titleStyle.Render(groups[gi].title))
			for _, b := range groups[gi].keys {
				lines = append(lines, "  "+m.pad(bindingLabel(b), 12)+" "+b.Help().Desc)
			}
		}
		return lines
	}
	indent := func(s string) string { // wrapped text, indented as a body
		lines := strings.Split(ansi.Wrap(s, max(m.width-2, 20), ""), "\n")
		for i := range lines {
			lines[i] = "  " + lines[i]
		}
		return strings.Join(lines, "\n") + "\n"
	}
	left, right := block(0, 1), block(2, 3)
	var b strings.Builder
	b.WriteString(titleStyle.Render("Sneakernet - help") + dimStyle.Render("   any key closes this") + "\n\n")
	if m.width >= 2*col+2 {
		for i := 0; i < max(len(left), len(right)); i++ {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			b.WriteString(m.pad(l, col) + r + "\n")
		}
	} else {
		for _, l := range append(left, right...) {
			b.WriteString(l + "\n")
		}
	}
	var quals []string
	for _, f := range search.Fields {
		quals = append(quals, f.Name+":")
	}
	b.WriteString("\n" + titleStyle.Render("Search") + "\n")
	b.WriteString(indent("Type to filter by name, then by type, host, SNI... Or limit it to one property: " +
		strings.Join(quals, " ") + " e.g. sec:reality port:443. Paste share links anywhere to add them."))
	b.WriteString(titleStyle.Render("Without the TUI") + "\n")
	b.WriteString(indent("Every action is also a command (handy with a screen reader): sneakernet list | switch <n|auto> | test --all | add | remove <n> | status"))
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, m.g.ellipsis)
	}
	return strings.Join(lines, "\n")
}
