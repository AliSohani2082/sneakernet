package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AliSohani2082/sneakernet/internal/manage"
)

func TestExitCodes(t *testing.T) {
	empty := t.TempDir() // a directory with nothing installed
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", nil, 2},
		{"unknown command", []string{"stauts"}, 2},
		{"unknown flag", []string{"status", "--nope"}, 2},
		{"bad flag value", []string{"test", "--timeout", "abc", "--root", empty}, 2},
		{"unknown flag of install", []string{"install", "--nope"}, 2},
		{"bad color value", []string{"--color=blue", "version"}, 2},
		{"switch without argument", []string{"switch", "--root", empty}, 2},
		{"remove with a word", []string{"remove", "abc", "--root", empty}, 2},
		{"tui without a terminal", []string{"tui", "--root", empty}, 2},
		{"not installed: status", []string{"status", "--root", empty}, 3},
		{"not installed: list", []string{"list", "--root", empty}, 3},
		{"not installed: tui is checked first for a terminal", []string{"tui"}, 2},
		{"help", []string{"help"}, 0},
		{"help of a command", []string{"help", "status"}, 0},
		{"-h of a command", []string{"status", "-h"}, 0},
		{"version", []string{"version"}, 0},
		{"failure", []string{"convert", "--servers", filepath.Join(empty, "missing")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := sn(t, "", tc.args...)
			if code != tc.want {
				t.Errorf("exit %d, want %d:\n%s", code, tc.want, out)
			}
		})
	}
}

func TestInterruptedExitsWith130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	code := run(ctx, []string{"convert", "--servers", filepath.Join(t.TempDir(), "missing")}, strings.NewReader(""), &out, &out)
	if code != 130 || !strings.Contains(out.String(), "interrupted") {
		t.Errorf("exit %d:\n%s", code, &out)
	}
}

func TestExitCodesAreDocumented(t *testing.T) {
	out, code := sn(t, "", "--help")
	if code != 0 {
		t.Fatal(code)
	}
	for _, want := range []string{"Exit codes:", "  0    success", "  1    the operation failed", "  2    usage",
		"  3    sneakernet is not installed", "  4    root is needed", "  5    no connectivity", "  130  interrupted",
		"--color auto|always|never", "--no-input", "--ascii", "stderr"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q\n%s", want, out)
		}
	}
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Exit codes", "`130`", "`--no-input`", "NO_COLOR"} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README lacks %q", want)
		}
	}
}

func TestResultsToStdoutProgressToStderr(t *testing.T) {
	stdout, stderr, code := snSplit(t, "", "convert", "--servers", "../../test/fixtures/servers.txt", "--server", "1")
	if code != 0 || !strings.HasPrefix(stdout, "{") || stderr != "" {
		t.Fatalf("convert (%d): stdout %.40q stderr %q", code, stdout, stderr)
	}

	stdout, stderr, code = snSplit(t, "", "nonsense")
	if code != 2 || stdout != "" || !strings.Contains(stderr, `unknown command "nonsense"`) {
		t.Errorf("errors go to stderr (%d): %q / %q", code, stdout, stderr)
	}
	_, stderr, _ = snSplit(t, "", "--color=never", "no-such")
	if strings.Contains(stderr, "\x1b[") {
		t.Errorf("--color=never left escape codes: %q", stderr)
	}

	root := t.TempDir()
	stdout, stderr, code = snSplit(t, "", "install", "--bundle", makeBundle(t), "--root", root, "--yes", "--server", "2",
		"--routing", "global")
	if code != 0 {
		t.Fatalf("install (%d):\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "bundle v0.1.0-test is intact") || strings.Contains(stdout, "is intact") {
		t.Errorf("step lines belong on stderr:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stdout, "SOCKS5 proxy") || strings.Contains(stderr, "SOCKS5 proxy") {
		t.Errorf("the summary belongs on stdout:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	stdout, stderr, code = snSplit(t, "", "list", "--root", root)
	if code != 0 || !strings.Contains(stdout, "vless") || stderr != "" {
		t.Errorf("list (%d): stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestInstallPrintsTheNonInteractiveCommand(t *testing.T) {
	b := makeBundle(t)
	out, code := sn(t, "", "install", "--bundle", b, "--root", t.TempDir(), "--yes", "--server", "2",
		"--routing", "bypass-region", "--region", "ir", "--socks-port", "20808", "--http-port", "20809")
	if code != 0 {
		t.Fatalf("install (%d):\n%s", code, out)
	}
	want := "sudo sh " + filepath.Join(b, "install.sh") +
		" --yes --server 2 --routing bypass-region --region ir --socks-port 20808 --http-port 20809"
	if !strings.Contains(out, "Next time, skip the questions:") || !strings.Contains(out, want) {
		t.Errorf("summary lacks %q:\n%s", want, out)
	}
}

func TestReinstallCommand(t *testing.T) {
	st := manage.DefaultState()
	st.UseAuto()
	st.Routing, st.Region = "bypass-region", "ir"
	if got, want := reinstallCommand([]string{"--arch", "amd64", "--bundle=/mnt/Ventoy/sneakernet"}, st),
		"sudo sh /mnt/Ventoy/sneakernet/install.sh --yes --server auto --routing bypass-region --region ir"; got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	st.Routing, st.Region = "global", "ir" // a leftover region is not repeated
	if got, want := reinstallCommand([]string{"--bundle", "/mnt/My Stick/sneakernet"}, st),
		"sudo sh '/mnt/My Stick/sneakernet/install.sh' --yes --server auto --routing global"; got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if got := reinstallCommand(nil, st); !strings.HasPrefix(got, "sudo sneakernet install --yes") {
		t.Errorf("without a bundle: %q", got)
	}
}

func TestPromptWithoutTerminalNamesTheFlag(t *testing.T) {
	// A real (non-terminal) stdin: a pipe, as in `echo | sneakernet install`.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	pw.Close()
	defer pr.Close()

	var out bytes.Buffer
	u := newUI(pr, &out, &out, globals{color: colorAuto})
	if u.interactive {
		t.Fatal("a pipe is not interactive")
	}
	for _, tc := range []struct{ title, flag string }{
		{"Which traffic should use the proxy?", "--routing"},
		{"Where should Sneakernet be installed?", "--target /"},
		{"Which interface?", "--yes"},
	} {
		_, err := u.choose(tc.title, []option{{label: "a"}, {label: "b"}}, 0)
		var ce *codedError
		if !errors.As(err, &ce) || ce.code != exitUsage || !strings.Contains(err.Error(), tc.flag) {
			t.Errorf("%q: want a usage error naming %s, got %v", tc.title, tc.flag, err)
		}
	}
	if out.Len() != 0 {
		t.Errorf("it must fail before printing the menu: %q", &out)
	}
	for _, tc := range []struct{ q, flag string }{
		{"Server", "--server"}, {"Two-letter country code (e.g. ir, cn)", "--region"},
		{"Remove Sneakernet, its service and /opt? (y/n)", "--yes"},
	} {
		if _, err := u.ask(tc.q, "x"); err == nil || !strings.Contains(err.Error(), tc.flag) {
			t.Errorf("ask %q: got %v, want it to name %s", tc.q, err, tc.flag)
		}
	}
	if _, err := u.readLine(); err == nil {
		t.Error("readLine must not read from a non-terminal")
	}

	// --yes still works without a terminal.
	u.yes = true
	if a, err := u.ask("Server", "auto"); err != nil || a != "auto" {
		t.Errorf("--yes: %q %v", a, err)
	}
}

func TestNoInputFlag(t *testing.T) {
	root := t.TempDir()
	b := makeBundle(t)
	stdout, stderr, code := snSplit(t, "1\n1\n", "install", "--bundle", b, "--root", root, "--no-input")
	if code != 2 || !strings.Contains(stderr, "--no-input") || !strings.Contains(stderr, "--yes") {
		t.Errorf("--no-input must refuse to ask even with a scripted stdin (%d):\n%s\n%s", code, stdout, stderr)
	}
	// With the answers given as flags it runs.
	_, stderr, code = snSplit(t, "", "install", "--bundle", b, "--root", root, "--no-input", "--yes", "--server", "auto")
	if code != 0 {
		t.Errorf("--no-input --yes (%d):\n%s", code, stderr)
	}
	// uninstall asks for confirmation: refuse, name --yes, change nothing.
	_, stderr, code = snSplit(t, "y\n", "uninstall", "--root", root, "--no-input")
	if code != 2 || !strings.Contains(stderr, "--yes") {
		t.Errorf("uninstall --no-input (%d): %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "opt")); err != nil {
		t.Errorf("a refused uninstall removed files: %v", err)
	}
}

func TestColorRule(t *testing.T) {
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
	for _, tc := range []struct {
		name string
		mode colorMode
		env  func(string) string
		tty  bool
		want bool
	}{
		{"terminal", colorAuto, env("TERM", "linux"), true, true},
		{"pipe", colorAuto, env("TERM", "linux"), false, false},
		{"NO_COLOR=1", colorAuto, env("TERM", "linux", "NO_COLOR", "1"), true, false},
		{"NO_COLOR=yes (the TUI used to ignore it)", colorAuto, env("TERM", "linux", "NO_COLOR", "yes"), true, false},
		{"NO_COLOR empty", colorAuto, env("TERM", "linux", "NO_COLOR", ""), true, true},
		{"dumb", colorAuto, env("TERM", "dumb"), true, false},
		{"always beats NO_COLOR and pipes", colorAlways, env("NO_COLOR", "1"), false, true},
		{"never beats a terminal", colorNever, env("TERM", "linux"), true, false},
	} {
		if got := colorEnabled(tc.mode, tc.env, tc.tty); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestColorFlags(t *testing.T) {
	var gl globals
	rest, gl, err := parseGlobals([]string{"--color", "never", "list", "--no-input", "--root", "x", "--ascii"}, func(string) string { return "" })
	if err != nil || gl.color != colorNever || !gl.noInput || !gl.ascii || strings.Join(rest, " ") != "list --root x" {
		t.Errorf("%v %+v %v", rest, gl, err)
	}
	if _, gl, _ = parseGlobals([]string{"--no-color", "status"}, func(string) string { return "" }); gl.color != colorNever {
		t.Error("--no-color")
	}
	if _, gl, _ = parseGlobals([]string{"status"}, func(k string) string {
		if k == "SNEAKERNET_COLOR" {
			return "always"
		}
		return ""
	}); gl.color != colorAlways {
		t.Error("SNEAKERNET_COLOR sets the default")
	}
	if _, gl, _ = parseGlobals([]string{"--color=never", "status"}, func(string) string { return "always" }); gl.color != colorNever {
		t.Error("the flag beats SNEAKERNET_COLOR")
	}
	if rest, _, _ = parseGlobals([]string{"add", "--", "--no-color"}, func(string) string { return "" }); strings.Join(rest, " ") != "add -- --no-color" {
		t.Errorf("flags after -- are kept: %v", rest)
	}

	// --color=always paints errors and status lines even into a buffer.
	out, _ := sn(t, "", "--color=always", "remove", "abc", "--root", t.TempDir())
	if !strings.Contains(out, "\x1b[31mERR\x1b[0m") {
		t.Errorf("no color with --color=always: %q", out)
	}
	out, _ = sn(t, "", "--color=never", "remove", "abc", "--root", t.TempDir())
	if strings.Contains(out, "\x1b") {
		t.Errorf("color with --color=never: %q", out)
	}
}

func TestSudoReexecKeepsColorAndAsciiChoices(t *testing.T) {
	var out bytes.Buffer
	t.Setenv("NO_COLOR", "1")
	u := newUI(strings.NewReader(""), &out, &out, globals{color: colorAuto, noInput: true, ascii: true})
	got := strings.Join(u.reexecGlobals(), " ")
	if got != "--color=never --no-input --ascii" {
		t.Errorf("NO_COLOR is dropped by sudo, so it must travel as flags: %q", got)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("SNEAKERNET_ASCII", "")
	u = newUI(strings.NewReader(""), &out, &out, globals{color: colorAlways})
	if got := strings.Join(u.reexecGlobals(), " "); got != "--color=always" {
		t.Errorf("got %q", got)
	}
	u = newUI(strings.NewReader(""), &out, &out, globals{color: colorAuto})
	if got := u.reexecGlobals(); len(got) != 0 {
		t.Errorf("nothing to repeat: %q", got)
	}
	// An ASCII console detected by the parent is repeated for the child
	// (sudo may drop LANG).
	t.Setenv("TERM", "linux")
	u = newUI(strings.NewReader(""), &out, &out, globals{color: colorAuto})
	if got := strings.Join(u.reexecGlobals(), " "); got != "--ascii" {
		t.Errorf("got %q", got)
	}
}

func TestHelpForACommand(t *testing.T) {
	want := []string{"usage: sneakernet switch <number|auto>", "Examples:",
		"sudo sneakernet switch auto    keep testing all servers", "Flags:", "-root", "Global flags", "sneakernet help"}
	help, code := sn(t, "", "help", "switch")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, w := range want {
		if !strings.Contains(help, w) {
			t.Errorf("help switch lacks %q:\n%s", w, help)
		}
	}
	if dash, _ := sn(t, "", "switch", "-h"); dash != help {
		t.Errorf("switch -h differs from help switch:\n%s", dash)
	}
	for _, c := range commandNames() {
		out, code := sn(t, "", "help", c)
		if code != 0 || !strings.HasPrefix(out, "usage: sneakernet "+c) {
			t.Errorf("help %s (%d):\n%s", c, code, out)
		}
	}
	if out, _ := sn(t, "", "help", "install"); !strings.Contains(out, "-routing") || !strings.Contains(out, "--yes") {
		t.Errorf("install help lacks its flags:\n%s", out)
	}
	// Every command named in the global help has a doc.
	usage, _ := sn(t, "", "help")
	for _, d := range docs {
		if !strings.Contains(usage, "  "+d.name) {
			t.Errorf("global help lacks %s", d.name)
		}
	}
}

func TestDidYouMean(t *testing.T) {
	for in, want := range map[string]string{
		"stauts": "status", "lst": "list", "swich": "switch", "instal": "install", "doctr": "doctor",
		"unistall": "uninstall", "tests": "test", "ad": "add",
	} {
		s := suggest(in, commandNames())
		if len(s) == 0 || s[0] != want {
			t.Errorf("suggest(%q) = %v, want %s first", in, s, want)
		}
	}
	if s := suggest("zzzzzz", commandNames()); len(s) != 0 {
		t.Errorf("nothing is close to zzzzzz: %v", s)
	}
	out, code := sn(t, "", "stauts")
	if code != 2 || !strings.Contains(out, `Did you mean "status"?`) {
		t.Errorf("(%d) %s", code, out)
	}
	out, code = sn(t, "", "help", "swich")
	if code != 2 || !strings.Contains(out, `Did you mean "switch"?`) {
		t.Errorf("(%d) %s", code, out)
	}
	if out, _ = sn(t, "", "zzzzzz"); strings.Contains(out, "Did you mean") {
		t.Errorf("no suggestion expected: %s", out)
	}
}

func TestBanner(t *testing.T) {
	lines := bannerLines()
	if len(lines) < 4 || len(lines) > 7 {
		t.Errorf("the art is %d lines tall", len(lines))
	}
	for _, s := range append(lines, bannerTagline, bannerCompact) {
		if len(s) > 78 {
			t.Errorf("%d columns: %q", len(s), s)
		}
		for _, r := range s {
			if r > 126 || (r < 32 && r != '\n') {
				t.Errorf("not 7-bit printable ASCII: %q in %q", r, s)
			}
		}
	}
	if len(bannerCompact) >= bannerMinWidth {
		t.Errorf("the compact line must fit below %d columns", bannerMinWidth)
	}

	if got := renderBanner(false, 80); !strings.HasPrefix(got, bannerArt) || strings.Contains(got, "\x1b") {
		t.Errorf("plain banner: %q", got)
	}
	if got := renderBanner(false, 59); !strings.HasPrefix(got, bannerCompact) || strings.Contains(got, "_ _") {
		t.Errorf("narrow terminals get the one-liner: %q", got)
	}
	if got := renderBanner(true, 80); !strings.Contains(got, "\x1b[1;36m") {
		t.Errorf("colored banner: %q", got)
	}
}

func TestBannerOnlyForPeopleAtATerminal(t *testing.T) {
	show := func(g globals, tty, yes bool) string {
		var out bytes.Buffer
		u := newUI(strings.NewReader(""), &out, &out, g)
		u.yes = yes
		u.bannerTo(&out, tty, 80)
		return out.String()
	}
	if got := show(globals{color: colorNever}, true, false); !strings.Contains(got, bannerArt) {
		t.Errorf("a terminal should get the banner: %q", got)
	}
	if got := show(globals{color: colorAlways}, true, false); !strings.Contains(got, "\x1b[1;36m") {
		t.Errorf("--color=always colors it: %q", got)
	}
	for name, got := range map[string]string{
		"not a terminal": show(globals{}, false, false),
		"--no-input":     show(globals{noInput: true}, true, false),
		"--yes":          show(globals{}, true, true),
	} {
		if got != "" {
			t.Errorf("%s must not print the banner: %q", name, got)
		}
	}

	// Through run(): buffers are never terminals, so no command prints it.
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"version"}, {"help", "status"}} {
		out, _ := sn(t, "", args...)
		if strings.Contains(out, "|_||_|") {
			t.Errorf("%v printed the banner into a pipe:\n%s", args, out)
		}
	}
	b := makeBundle(t)
	out, code := sn(t, "", "install", "--bundle", b, "--root", t.TempDir(), "--yes")
	if code != 0 || strings.Contains(out, "|_||_|") {
		t.Errorf("install (%d) printed the banner:\n%s", code, out)
	}
}
