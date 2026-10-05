package tui

import "charm.land/bubbles/v2/key"

// keyMap holds every binding; the help bar is rendered from it. On the
// search screen letters go to the search box, so actions use ctrl keys.
type keyMap struct {
	Up, Down, PageUp, PageDown key.Binding

	Use, Test, Best, Auto, Add, Remove, Sort, Logs, Restart, Power, Clear, Quit key.Binding

	Save, Cancel key.Binding // add screen

	Refresh, Back key.Binding // logs screen
}

func newKeyMap() keyMap {
	b := func(help, desc string, keys ...string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(help, desc))
	}
	return keyMap{
		Up:       b("↑", "up", "up"),
		Down:     b("↓", "down", "down"),
		PageUp:   b("pgup", "page up", "pgup"),
		PageDown: b("pgdn", "page down", "pgdown"),

		Use:     b("enter", "use", "enter"),
		Test:    b("^t", "test results", "ctrl+t"),
		Best:    b("^b", "use fastest", "ctrl+b"),
		Auto:    b("^a", "auto", "ctrl+a"),
		Add:     b("^n", "add", "ctrl+n"),
		Remove:  b("^x", "remove", "ctrl+x"),
		Sort:    b("^o", "sort", "ctrl+o"),
		Logs:    b("^l", "logs", "ctrl+l"),
		Restart: b("^r", "restart", "ctrl+r"),
		Power:   b("^s", "start/stop", "ctrl+s"),
		Clear:   b("esc", "clear/quit", "esc"),
		Quit:    b("^c", "quit", "ctrl+c"),

		Save:   b("^s", "save", "ctrl+s"),
		Cancel: b("esc", "cancel", "esc"),

		Refresh: b("r", "refresh", "r"),
		Back:    b("esc", "back", "esc", "q", "ctrl+l"),
	}
}

func (k keyMap) searchHelp() []key.Binding {
	return []key.Binding{k.Use, k.Test, k.Best, k.Auto, k.Add, k.Remove, k.Sort, k.Logs, k.Restart, k.Power, k.Clear}
}

func (k keyMap) addHelp() []key.Binding { return []key.Binding{k.Save, k.Cancel, k.Quit} }

func (k keyMap) logsHelp() []key.Binding { return []key.Binding{k.Refresh, k.Back, k.Quit} }

// actions are the bindings that start work and so wait while busy.
func (k keyMap) actions() []key.Binding {
	return []key.Binding{k.Use, k.Test, k.Best, k.Auto, k.Remove, k.Restart, k.Power}
}
