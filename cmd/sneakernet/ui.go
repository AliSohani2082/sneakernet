package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"

	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/tui"
)

// errInputClosed means stdin ended while a question was open.
var errInputClosed = errors.New("input closed before the question was answered")

// ui writes progress and asks questions on a plain terminal. It works over
// serial consoles and pipes, so tests and VMs can script it. Results go to
// out; progress, warnings and errors go to err.
type ui struct {
	in    *bufio.Reader
	out   io.Writer
	err   io.Writer
	color bool // paint out
	// errColor is the same for err; the streams can differ (2>/dev/null).
	errColor bool
	yes      bool // accept every default without asking
	log      io.Writer

	g           globals
	args        []string // the command line after the global flags
	interactive bool     // questions may be asked
	inTTY       bool     // stdin is a terminal
	outTTY      bool     // stdout is a terminal
	ascii       bool     // console-safe output was asked for or detected
}

func newUI(in io.Reader, out, errOut io.Writer, g globals) *ui {
	u := &ui{in: bufio.NewReader(in), out: out, err: errOut, log: io.Discard, g: g}
	u.color = colorEnabled(g.color, os.Getenv, isTerminal(out))
	u.errColor = colorEnabled(g.color, os.Getenv, isTerminal(errOut))
	u.inTTY, u.outTTY = isTerminal(in), isTerminal(out)
	// A reader that is not a file is a script feeding answers (tests); a real
	// stdin that is not a terminal is a pipe or /dev/null, and nobody is there
	// to answer.
	u.interactive = !g.noInput
	if _, isFile := in.(*os.File); isFile && !u.inTTY {
		u.interactive = false
	}
	u.ascii = g.ascii || tui.DetectASCII(os.Getenv)
	return u
}

// isTerminal reports whether w (an io.Reader or io.Writer) is a terminal.
func isTerminal(w any) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}

func (u *ui) paint(code, s string) string { return paintIf(u.color, code, s) }

func paintIf(on bool, code, s string) string {
	if !on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (u *ui) bold(s string) string { return u.paint("1", s) }
func (u *ui) dim(s string) string  { return u.paint("2", s) }

// printf writes a result (or a prompt) to stdout and the install log.
func (u *ui) printf(format string, a ...any) {
	fmt.Fprintf(u.out, format, a...)
	fmt.Fprintf(u.log, format, a...)
}

// notef writes a dimmed side note to stderr and the install log.
func (u *ui) notef(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(u.err, "%s\n", paintIf(u.errColor, "2", msg))
	fmt.Fprintf(u.log, "%s\n", msg)
}

// line writes a status line to stderr: progress is not the result of a
// command, so it must not end up in a pipe.
func (u *ui) line(prefix, code, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(u.err, "%s %s\n", paintIf(u.errColor, code, prefix), msg)
	fmt.Fprintf(u.log, "%s %s\n", prefix, msg)
}

func (u *ui) step(format string, a ...any) { u.line("==>", "1;36", format, a...) }
func (u *ui) ok(format string, a ...any)   { u.line(" ok", "32", format, a...) }
func (u *ui) warn(format string, a ...any) { u.line(" !!", "33", format, a...) }
func (u *ui) fail(format string, a ...any) { u.line("ERR", "31", format, a...) }

// logf goes to the log file only.
func (u *ui) logf(format string, a ...any) { fmt.Fprintf(u.log, "    "+format+"\n", a...) }

// ask prints a question and returns the trimmed answer, or def when the
// answer is empty or --yes is set.
func (u *ui) ask(question, def string) (string, error) {
	hint := ""
	if def != "" {
		hint = " [" + def + "]"
	}
	if err := u.needInput(question); err != nil {
		return "", err
	}
	fmt.Fprintf(u.out, "%s%s: ", question, hint)
	if u.yes {
		fmt.Fprintln(u.out, def)
		return def, nil
	}
	s, err := u.in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		fmt.Fprintln(u.out)
		return "", errInputClosed
	}
	s = strings.TrimSpace(s)
	if s == "" {
		s = def
	}
	fmt.Fprintf(u.log, "%s%s: %s\n", question, hint, s)
	return s, nil
}

// flagFor names the flag that answers a question, so a script that cannot
// type gets told what to pass. Matched on lower-case substrings, first wins.
var flagFor = []struct{ match, flag string }{
	{"interface", "--yes"},
	{"installed?", "--target /"},
	{"traffic", "--routing bypass-lan|bypass-region|global"},
	{"country code", "--region <two-letter code>"},
	{"keep these settings", "--server and --routing"},
	{"server", "--server auto|<number>"},
	{"remove sneakernet", "--yes"},
}

// needInput fails when a question has to be asked but nobody can answer.
func (u *ui) needInput(question string) error {
	if u.yes || u.interactive {
		return nil
	}
	flag := "--yes"
	q := strings.ToLower(question)
	for _, f := range flagFor {
		if strings.Contains(q, f.match) {
			flag = f.flag
			break
		}
	}
	why := "stdin is not a terminal"
	if u.g.noInput {
		why = "--no-input is set"
	}
	return usageErr("cannot ask %q: %s. Pass %s (or --yes to accept the defaults)",
		strings.TrimSpace(question), why, flag)
}

// readLine reads one line of input without a prompt. The text is not logged.
func (u *ui) readLine() (string, error) {
	if !u.interactive {
		return "", errInputClosed
	}
	s, err := u.in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		return "", errInputClosed
	}
	s = strings.TrimSpace(s)
	// Pasted share links carry UUIDs and passwords: never copy them to the log.
	fmt.Fprintf(u.log, "(input line, %d characters, not logged)\n", len(s))
	return s, nil
}

func (u *ui) confirm(question string, def bool) (bool, error) {
	d := "y"
	if !def {
		d = "n"
	}
	for {
		a, err := u.ask(question+" (y/n)", d)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(a) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		u.warn("please answer y or n")
	}
}

type option struct {
	label    string
	note     string
	disabled string // reason this option cannot be picked now
}

// choose shows a numbered menu and returns the 0-based choice.
func (u *ui) choose(title string, opts []option, def int) (int, error) {
	if err := u.needInput(title); err != nil {
		return 0, err
	}
	u.printf("\n%s\n", u.bold(title))
	for i, o := range opts {
		label := fmt.Sprintf("  %d) %s", i+1, o.label)
		switch {
		case o.disabled != "":
			u.printf("%s\n", u.dim(label+" — "+o.disabled))
		case o.note != "":
			u.printf("%s %s\n", label, u.dim("— "+o.note))
		default:
			u.printf("%s\n", label)
		}
	}
	for {
		a, err := u.ask("Choose", strconv.Itoa(def+1))
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(a)
		if err != nil || n < 1 || n > len(opts) {
			u.warn("enter a number from 1 to %d", len(opts))
			continue
		}
		if r := opts[n-1].disabled; r != "" {
			u.warn("%s is not available: %s", opts[n-1].label, r)
			continue
		}
		return n - 1, nil
	}
}

// serverTable prints the server list. mark is the active server index.
func (u *ui) serverTable(servers []links.Server, mark int) {
	u.printf("%s\n", u.bold(fmt.Sprintf("  %4s  %-40s  %-26s  %s", "#", "name", "type", "note")))
	for _, s := range servers {
		cur := " "
		if s.Index == mark {
			cur = "*"
		}
		row := fmt.Sprintf("%s %4d  %-40s  %-26s", cur, s.Index, clip(s.Name, 40), s.Kind())
		switch {
		case !s.Usable():
			u.printf("%s\n", u.dim(row+"  "+s.Problem))
		case len(s.Warnings) > 0:
			u.printf("%s  %s\n", row, u.dim(s.Warnings[0]))
		default:
			u.printf("%s\n", row)
		}
	}
}

// clip shortens s to n runes, padding nothing; table widths use %-Ns.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
