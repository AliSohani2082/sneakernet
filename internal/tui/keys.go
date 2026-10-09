package tui

import "charm.land/bubbles/v2/key"

// keyMap holds every binding; the help bars are rendered from it. On the
// search screen letters go to the search box, so actions use ctrl keys, each
// with an F-key alias: F-keys work on the Linux console, and inside tmux or
// screen the ctrl chords ^b and ^a belong to the multiplexer.
type keyMap struct {
	Up, Down, PageUp, PageDown key.Binding

	Use, Test, Best, Auto, Add, Remove, Sort, Logs, Restart, Power, Clear, Quit, Help key.Binding

	Save, Cancel key.Binding // add screen

	Refresh, Back key.Binding // logs screen
}

func newKeyMap(g glyphs, fkeys bool) keyMap {
	b := func(help, desc string, keys ...string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	// The Linux console sends F1-F5 as ESC [ [ A..E, which Bubble Tea does not
	// decode, so there the bar names the chord that works.
	helpKey := "F1"
	if !fkeys {
		helpKey = "^g"
	}
	return keyMap{
		Up:       b(g.up, "up", "up"),
		Down:     b(g.down, "down", "down"),
		PageUp:   b("pgup", "page up", "pgup"),
		PageDown: b("pgdn", "page down", "pgdown"),

		Use:     b("enter", "use", "enter"),
		Test:    b("^t", "test results", "ctrl+t", "f5"),
		Best:    b("^b", "use fastest", "ctrl+b", "f6"),
		Auto:    b("^a", "auto", "ctrl+a", "f7"),
		Add:     b("^n", "add", "ctrl+n", "f2"),
		Remove:  b("^x", "remove", "ctrl+x", "f8"),
		Sort:    b("^o", "sort", "ctrl+o"),
		Logs:    b("^l", "logs", "ctrl+l", "f3"),
		Restart: b("^r", "restart", "ctrl+r"),
		Power:   b("^s", "start/stop", "ctrl+s"),
		Clear:   b("esc", "clear/quit", "esc"),
		Quit:    b("^c", "quit", "ctrl+c", "f10"),
		Help:    b(helpKey, "help", "f1", "ctrl+g"),

		Save:   b("^s", "save", "ctrl+s"),
		Cancel: b("esc", "cancel", "esc"),

		Refresh: b("r", "refresh", "r"),
		Back:    b("esc", "back", "esc", "q", "ctrl+l", "f3"),
	}
}

// searchHelp is the short bar of the search screen: what is used most, and
// the way to the full help. Disabled bindings are skipped by the help bubble.
func (k keyMap) searchHelp() []key.Binding {
	return []key.Binding{k.Use, k.Test, k.Best, k.Add, k.Clear, k.Help}
}

func (k keyMap) addHelp() []key.Binding { return []key.Binding{k.Save, k.Cancel, k.Quit, k.Help} }

func (k keyMap) logsHelp() []key.Binding { return []key.Binding{k.Refresh, k.Back, k.Quit, k.Help} }

// fullHelp are the groups of the help overlay.
func (k keyMap) fullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Clear},
		{k.Use, k.Test, k.Best, k.Auto, k.Sort},
		{k.Add, k.Remove, k.Logs},
		{k.Restart, k.Power, k.Help, k.Quit},
	}
}

// actions are the bindings that start work and so wait while busy.
func (k keyMap) actions() []key.Binding {
	return []key.Binding{k.Use, k.Test, k.Best, k.Auto, k.Remove, k.Restart, k.Power}
}
