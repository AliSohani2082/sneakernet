package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

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
	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

func (m *model) rule() string { return dimStyle.Render(strings.Repeat("─", max(m.width, 10))) }

func (m *model) header() string {
	state := dimStyle.Render("…")
	switch {
	case m.status.Active:
		state = okStyle.Render("● " + m.status.State)
	case m.status.State != "":
		state = errStyle.Render("○ " + m.status.State)
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
		dimStyle.Render(fmt.Sprintf("SOCKS 127.0.0.1:%d · HTTP 127.0.0.1:%d", m.state.SocksPort, m.state.HTTPPort)),
		using, dimStyle.Render("· routing "+routing))
}

func (m *model) searchView() string {
	var b strings.Builder
	w := m.width
	b.WriteString(m.header())

	count := fmt.Sprintf("%d of %d · by %s", len(m.hits), len(m.servers), m.order)
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
				b.WriteString(dimStyle.Render("  no servers match — esc clears the search, ^n adds servers") + "\n")
				continue
			}
			b.WriteString("\n")
			continue
		}
		b.WriteString(ansi.Truncate(m.row(m.hits[i], i == m.cursor, nameW), w, "") + "\n")
	}
	b.WriteString(m.rule() + "\n")
	b.WriteString(ansi.Truncate(m.details(), w, "…") + "\n")
	b.WriteString(m.statusLine() + "\n")
	b.WriteString(m.help.ShortHelpView(m.keys.searchHelp()))
	return b.String()
}

func (m *model) row(h search.Hit, selected bool, nameW int) string {
	s := &m.servers[h.Pos]
	cur := "  "
	if selected {
		cur = cursorStyle.Render("› ")
	}
	active := " "
	if m.mgr.Configured() && !m.state.Auto && s.Key() == m.state.Key {
		active = okStyle.Render("●")
	}
	base := lipgloss.NewStyle()
	if selected {
		base = base.Bold(true)
	}
	if !s.Usable() {
		base = base.Faint(true)
	}
	name := pad(lipgloss.StyleRunes(s.Name, h.NameRunes, matchStyle, base), nameW)
	kind := s.Kind()
	if h.Field != "" {
		kind += " ·" + h.Field
	}
	return fmt.Sprintf("%s%s%4d  %s  %s  %s", cur, active, s.Index, name,
		base.Faint(true).Render(pad(kind, kindW)), m.note(s))
}

// note is the right-hand column: test result, or why a server is unusable.
func (m *model) note(s *links.Server) string {
	if r, ok := m.results[s.Key()]; ok {
		if r.OK() {
			return okStyle.Render(fmt.Sprintf("%5d ms", r.Latency.Milliseconds()))
		}
		return errStyle.Render(ansi.Truncate("failed: "+r.Err.Error(), 16, "…"))
	}
	if !s.Usable() {
		return dimStyle.Render(ansi.Truncate(s.Problem, 16, "…"))
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
	line := dimStyle.Render(strings.Join(parts, " · "))
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
		return warnStyle.Render(ansi.Truncate(m.confirm.prompt, w, "…"))
	case m.busy != "":
		return m.spin.View() + " " + ansi.Truncate(m.msg, w-2, "…")
	case m.msg != "":
		style := okStyle
		if m.msgErr {
			style = errStyle
		}
		return style.Render(ansi.Truncate(m.msg, w, "…"))
	}
	return ""
}

func (m *model) addView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Sneakernet — add servers") + "\n")
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
	return ansi.Truncate(strings.Join(parts, " · "), m.width, "…")
}

func (m *model) logsView() string {
	return m.header() + m.rule() + "\n" + m.logs.View() + "\n" + m.help.ShortHelpView(m.keys.logsHelp())
}

// pad fits s into exactly n terminal cells (emoji flags are two cells wide).
func pad(s string, n int) string {
	s = ansi.Truncate(s, n, "…")
	if w := ansi.StringWidth(s); w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}
