package tui

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// plainRoot is an installation with only a server list and a state file, so
// the tests do not need an Xray binary. There is no service manager.
func plainRoot(t *testing.T, servers string) *manage.Manager {
	t.Helper()
	tg := target.Dir(t.TempDir())
	p := tg.Path(layout.ServersFile)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(servers), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := &manage.Manager{T: tg}
	if err := mgr.SaveState(manage.DefaultState()); err != nil {
		t.Fatal(err)
	}
	return mgr
}

const uxServers = `vless://a8d31bbb-0d00-4762-b870-8c23e19d0a8c@de.example.com:443?type=ws&security=tls&sni=de.example.com&path=%2Fws#🇩🇪 Berlin fast
vless://a8d31bbb-0d00-4762-b870-8c23e19d0a8c@nl.example.com:443?type=grpc&security=tls&serviceName=g#🇳🇱 Amsterdam
trojan://pw@tr.example.com:443#Istanbul trojan
vmess://eyJ2IjoiMiIsInBzIjoiYSIsImFkZCI6ImEuZXhhbXBsZS5jb20iLCJwb3J0IjoiNDQzIiwiaWQiOiJhOGQzMWJiYi0wZDAwLTQ3NjItYjg3MC04YzIzZTE5ZDBhOGMiLCJuZXQiOiJ3cyIsInRscyI6InRscyJ9
`

func uxModel(t *testing.T, opts Options, w, h int) *model {
	t.Helper()
	return newSizedModel(t, plainRoot(t, uxServers), opts, w, h)
}

func press(m *model, code rune) {
	m.Update(tea.KeyPressMsg{Code: code})
}

func TestDetectASCII(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	for name, tc := range map[string]struct {
		env  func(string) string
		want bool
	}{
		"linux console":       {env("TERM", "linux", "LANG", "en_US.UTF-8"), true},
		"explicit":            {env("SNEAKERNET_ASCII", "1", "TERM", "xterm-256color", "LANG", "en_US.UTF-8"), true},
		"C locale":            {env("TERM", "xterm", "LANG", "C"), true},
		"POSIX via LC_ALL":    {env("TERM", "xterm", "LC_ALL", "POSIX", "LANG", "en_US.UTF-8"), true},
		"latin1":              {env("TERM", "xterm", "LANG", "de_DE.ISO-8859-1"), true},
		"utf-8":               {env("TERM", "xterm-256color", "LANG", "en_US.UTF-8"), false},
		"utf8 spelling":       {env("TERM", "xterm", "LC_CTYPE", "en_US.utf8"), false},
		"LC_ALL wins":         {env("TERM", "xterm", "LC_ALL", "en_US.UTF-8", "LANG", "C"), false},
		"locale unset (sudo)": {env("TERM", "xterm-256color"), false},
	} {
		if got := DetectASCII(tc.env); got != tc.want {
			t.Errorf("%s: DetectASCII = %v, want %v", name, got, tc.want)
		}
	}
}

func TestGlyphSelection(t *testing.T) {
	u, a := pickGlyphs(false), pickGlyphs(true)
	if u.ascii || !a.ascii {
		t.Fatal("ascii flag")
	}
	if u.spinner.Frames[0] != "⠋" || a.spinner.Frames[0] != "|" {
		t.Errorf("spinners: %q %q", u.spinner.Frames, a.spinner.Frames)
	}
	for _, s := range []string{a.cursor, a.active, a.inactive, a.sep, a.dot, a.rule, a.ellipsis, a.up, a.down} {
		if s != strings.Map(func(r rune) rune {
			if r > 127 {
				return -1
			}
			return r
		}, s) {
			t.Errorf("ascii glyph %q is not ASCII", s)
		}
	}
}

func TestAsciiNameFlags(t *testing.T) {
	got, matches := asciiName("🇩🇪 Berlin 👨‍👩‍👧 ok", []int{0, 3})
	if got != "[DE] Berlin ? ? ? ok" && !strings.HasPrefix(got, "[DE] Berlin") {
		t.Fatalf("name %q", got)
	}
	if len(matches) < 5 || matches[0] != 0 || matches[3] != 3 {
		t.Errorf("a match on the flag highlights all of [DE]; matches %v", matches)
	}
	if got, _ := asciiName("plain — name…", nil); got != "plain — name…" {
		t.Errorf("only emoji and flags change in names: %q", got)
	}
	if g := ttyGlyphs.safe("a — b… · ● 🇩🇪"); g != "a - b... | * [DE]" {
		t.Errorf("safe: %q", g)
	}
	if g := unicodeGlyphs.safe("a — b… 🇩🇪"); g != "a — b… 🇩🇪" {
		t.Errorf("unicode mode must not change text: %q", g)
	}
}

func TestAsciiModeDrawsOnlyASCII(t *testing.T) {
	m := uxModel(t, Options{ASCII: true}, 80, 24)
	m.status.Active, m.status.State = true, "active"
	screens := map[string]func(){
		"search": func() {},
		"typed":  func() { typeText(t, m, "berlin") },
		"help":   func() { press(m, tea.KeyF1) },
	}
	for _, name := range []string{"search", "typed", "help"} {
		screens[name]()
		v := view(m)
		for _, r := range v {
			if r > 127 {
				t.Fatalf("%s screen has %q (U+%04X) in ASCII mode:\n%s", name, r, r, v)
			}
		}
		if name == "search" && !strings.Contains(v, "[DE] Berlin fast") {
			t.Errorf("flag should become [DE]:\n%s", v)
		}
	}
	// The unicode mode keeps the flag.
	if v := view(uxModel(t, Options{}, 80, 24)); !strings.Contains(v, "🇩🇪 Berlin fast") || !strings.Contains(v, "›") {
		t.Errorf("unicode mode:\n%s", v)
	}
}

func TestSpinnerFollowsMode(t *testing.T) {
	for ascii, want := range map[bool]string{true: "|", false: "⠋"} {
		m := uxModel(t, Options{ASCII: ascii}, 80, 24)
		m.startBusy("testing")
		if v := m.statusLine(); !strings.Contains(ansi.Strip(v), want) {
			t.Errorf("ascii=%v: spinner %q missing in %q", ascii, want, v)
		}
	}
}

func TestHelpOverlayToggle(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyF1}, ctrl('g')} {
		m := uxModel(t, Options{}, 80, 30)
		bar := lastLine(view(m))
		if !strings.HasSuffix(strings.TrimSpace(bar), "F1 help") {
			t.Errorf("the short bar should end with F1 help: %q", bar)
		}
		m.Update(k)
		v := view(m)
		if !m.showHelp {
			t.Fatalf("%v did not open the help", k)
		}
		for _, want := range []string{"Move", "Servers", "Service and app", "test results", "^t / F5",
			"use fastest", "^b / F6", "sec:", "port:", "sneakernet list"} {
			if !strings.Contains(v, want) {
				t.Errorf("help lacks %q:\n%s", want, v)
			}
		}
		m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
		if m.showHelp || m.query.Value() != "" {
			t.Errorf("a key closes the help and is not typed (help %v, query %q)", m.showHelp, m.query.Value())
		}
	}
	// ctrl+c still quits from the help.
	m := uxModel(t, Options{}, 80, 30)
	press(m, tea.KeyF1)
	if _, cmd := m.Update(ctrl('c')); cmd == nil {
		t.Error("ctrl+c should quit while help is open")
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestShortBarFitsAt80Columns(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		m := uxModel(t, Options{ASCII: ascii}, 80, 24)
		bar := lastLine(view(m))
		if strings.Contains(bar, m.g.ellipsis) {
			t.Errorf("ascii=%v: the bar is cut off: %q", ascii, bar)
		}
		if w := ansi.StringWidth(bar); w > 80 {
			t.Errorf("bar is %d cells wide", w)
		}
	}
}

func TestHelpBarColorsAreANSI(t *testing.T) {
	m := uxModel(t, Options{}, 80, 24)
	raw := m.help.ShortHelpView(m.keys.searchHelp())
	if strings.Contains(raw, "38;2;") || strings.Contains(raw, "38;5;") {
		t.Errorf("help bar uses RGB/256 colors: %q", raw)
	}
}

func TestContextAwareBindings(t *testing.T) {
	m := uxModel(t, Options{}, 100, 24)
	if !m.keys.Test.Enabled() || !m.keys.Remove.Enabled() {
		t.Fatal("with servers listed, test and remove work")
	}
	typeText(t, m, "zzzzqq")
	if len(m.hits) != 0 {
		t.Fatalf("hits %v", names(m))
	}
	if m.keys.Test.Enabled() || m.keys.Best.Enabled() || m.keys.Remove.Enabled() || m.keys.Use.Enabled() {
		t.Error("no results: test, use fastest, remove and use are disabled")
	}
	bar := lastLine(view(m))
	if strings.Contains(bar, "test results") || strings.Contains(bar, "use fastest") {
		t.Errorf("disabled bindings are hidden from the bar: %q", bar)
	}
	// Pressing a disabled action says why instead of silently doing nothing.
	m.Update(ctrl('t'))
	if !strings.Contains(m.msg, "no usable servers") || !m.msgErr {
		t.Errorf("msg %q", m.msg)
	}

	m.query.SetValue("")
	m.refresh()
	m.Update(statusMsg{service.Status{Active: true, State: "active"}})
	if d := m.keys.Power.Help().Desc; d != "stop" {
		t.Errorf("running service: power is %q", d)
	}
	m.Update(statusMsg{service.Status{State: "inactive"}})
	if d := m.keys.Power.Help().Desc; d != "start" {
		t.Errorf("stopped service: power is %q", d)
	}
}

func TestFKeyAliases(t *testing.T) {
	m := uxModel(t, Options{}, 100, 24)
	for k, b := range map[rune]key.Binding{
		tea.KeyF1: m.keys.Help, tea.KeyF2: m.keys.Add, tea.KeyF3: m.keys.Logs, tea.KeyF5: m.keys.Test,
		tea.KeyF6: m.keys.Best, tea.KeyF7: m.keys.Auto, tea.KeyF8: m.keys.Remove, tea.KeyF10: m.keys.Quit,
	} {
		if !key.Matches(tea.KeyPressMsg{Code: k}, b) {
			t.Errorf("%v does not match %v", k, b.Help())
		}
	}
	press(m, tea.KeyF2)
	if m.screen != addScreen {
		t.Error("F2 should open the add screen")
	}
}

func TestEscCancelsBusyWork(t *testing.T) {
	m := uxModel(t, Options{}, 100, 24)
	ctx, _ := m.startBusy("testing")
	m.setMsg(false, "testing 4 servers…")
	if v := view(m); !strings.Contains(v, "esc cancel") {
		t.Errorf("the status line should offer esc:\n%s", v)
	}
	m.syncKeys()
	if d := m.keys.Clear.Help().Desc; d != "cancel" {
		t.Errorf("the bar should say esc cancels: %q", d)
	}
	press(m, tea.KeyEscape)
	if ctx.Err() == nil {
		t.Fatal("esc did not cancel the work's context")
	}
	if m.busy == "" || !m.cancelling {
		t.Error("stays busy until the work reports back")
	}
	if m.query.Value() != "" || m.msg != "cancelling…" {
		t.Errorf("msg %q", m.msg)
	}
	// The work reports back; whatever it found is dropped.
	m.Update(testedMsg{err: ctx.Err()})
	if m.busy != "" || m.cancel != nil || m.cancelling {
		t.Errorf("busy state not reset: %q", m.busy)
	}
	if !strings.Contains(m.msg, "cancelled") || m.msgErr || len(m.results) != 0 || m.order != byRelevance {
		t.Errorf("msg %q err %v results %d", m.msg, m.msgErr, len(m.results))
	}
	if m.keys.Clear.Help().Desc != "clear/quit" {
		t.Error("esc is back to clear/quit")
	}
}

func TestEscCancelsSwitch(t *testing.T) {
	m := uxModel(t, Options{}, 100, 24)
	ctx, _ := m.startBusy("switching")
	press(m, tea.KeyEscape)
	m.Update(switchedMsg{err: ctx.Err()})
	if !strings.Contains(m.msg, "cancelled") || m.msgErr {
		t.Errorf("a cancelled switch is not an error: %q (err %v)", m.msg, m.msgErr)
	}
	// Removing is not cancellable: esc keeps its normal meaning.
	m.startUncancellable("removing")
	typeText(t, m, "x")
	press(m, tea.KeyEscape)
	if m.query.Value() != "" || m.cancelling {
		t.Error("esc during an uncancellable operation clears the search as usual")
	}
}

func TestMessagesFadeButErrorsStay(t *testing.T) {
	m := uxModel(t, Options{}, 100, 24)
	m.msgTTL = time.Second
	_, cmd := m.Update(ctrl('o'))
	if m.msg == "" || cmd == nil {
		t.Fatalf("a notice should schedule its own removal (msg %q)", m.msg)
	}
	id := m.msgID
	m.Update(msgClearMsg{id: id - 1})
	if m.msg == "" {
		t.Error("a stale timer must not clear a newer message")
	}
	m.Update(msgClearMsg{id: id})
	if m.msg != "" {
		t.Errorf("the notice should fade, still %q", m.msg)
	}

	m.setMsg(true, "boom")
	_, cmd = m.Update(statusMsg{})
	if cmd != nil || m.msg != "boom" {
		t.Error("an error is not scheduled to fade")
	}
	m.Update(msgClearMsg{id: m.msgID})
	// (a clear for an error id never arrives, but even then a key press is what clears it)
	m.setMsg(true, "boom")
	typeText(t, m, "a")
	if m.msg != "" {
		t.Errorf("a key press clears the error, still %q", m.msg)
	}

	// A timer firing while busy leaves the busy message alone.
	m.startBusy("testing")
	m.setMsg(false, "testing…")
	m.Update(msgClearMsg{id: m.msgID})
	if m.msg == "" {
		t.Error("busy message cleared")
	}
}

func TestDiscardPastedLinksAsksFirst(t *testing.T) {
	m := uxModel(t, Options{}, 100, 30)
	press(m, tea.KeyF2)
	m.Update(tea.PasteMsg{Content: "trojan://pw@new.example.com:443#brand new\n"})
	if m.plan == nil || len(m.plan.New) != 1 {
		t.Fatalf("plan %+v", m.plan)
	}
	press(m, tea.KeyEscape)
	if m.confirm == nil || m.screen != addScreen || !strings.Contains(view(m), "Discard 1 pasted link") {
		t.Fatalf("esc should ask before discarding:\n%s", view(m))
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.screen != addScreen || m.plan == nil || len(m.plan.New) != 1 {
		t.Error("answering n keeps the pasted links")
	}
	press(m, tea.KeyEscape)
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.screen != searchScreen {
		t.Error("answering y leaves the add screen")
	}

	// Nothing pasted: esc leaves at once.
	press(m, tea.KeyF2)
	press(m, tea.KeyEscape)
	if m.confirm != nil || m.screen != searchScreen {
		t.Error("an empty add screen closes without asking")
	}
}

func TestProgramOptionsHonourNoColor(t *testing.T) {
	if a, b := len(programOptions(t.Context(), Options{})), len(programOptions(t.Context(), Options{NoColor: true})); b != a+1 {
		t.Errorf("NoColor should add a color profile option: %d vs %d", a, b)
	}
}

// TestInstallerHintNamesARealKey keeps the installer's "press X to test"
// text in step with the key map (it once said T while the key was ^t).
func TestInstallerHintNamesARealKey(t *testing.T) {
	src, err := os.ReadFile("../../cmd/sneakernet/install.go")
	if err != nil {
		t.Skip(err)
	}
	mm := regexp.MustCompile(`press (\S+) to test them all`).FindSubmatch(src)
	if mm == nil {
		t.Fatal("the installer hint is gone")
	}
	if got, want := string(mm[1]), newKeyMap(unicodeGlyphs, true).Test.Help().Key; got != want {
		t.Errorf("hint says press %s but the key is %s", got, want)
	}
}

// TestGolden80Columns renders the search screen at 80x24 in both modes and
// compares it with testdata. Run with -update to rewrite them.
func TestGolden80Columns(t *testing.T) {
	port := regexp.MustCompile(`127\.0\.0\.1:\d+`)
	for name, ascii := range map[string]bool{"unicode": false, "ascii": true} {
		t.Run(name, func(t *testing.T) {
			m := uxModel(t, Options{ASCII: ascii}, 80, 24)
			m.status.Active, m.status.State = true, "active"
			m.state.Routing = "bypass-lan"
			got := port.ReplaceAllString(view(m), "127.0.0.1:PORT")
			// the help overlay too
			press(m, tea.KeyF1)
			got += "\n==== F1 ====\n" + port.ReplaceAllString(view(m), "127.0.0.1:PORT")
			path := filepath.Join("testdata", "view80-"+name+".golden")
			if *update {
				os.MkdirAll("testdata", 0o755)
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if string(want) != got {
				t.Errorf("view changed; run with -update if intended.\n--- got\n%s\n--- want\n%s", got, want)
			}
			for _, line := range strings.Split(got, "\n") {
				if w := ansi.StringWidth(line); w > 80 {
					t.Errorf("line is %d cells wide: %q", w, line)
				}
			}
		})
	}
}

func TestHelpKeyOnTheLinuxConsole(t *testing.T) {
	m := uxModel(t, Options{ASCII: true, NoFKeys: true}, 80, 24)
	bar := lastLine(view(m))
	if !strings.HasSuffix(strings.TrimSpace(bar), "^g help") {
		t.Errorf("without F-keys the bar names ^g: %q", bar)
	}
	m.Update(ctrl('g'))
	if !m.showHelp {
		t.Error("^g opens the help")
	}
}
