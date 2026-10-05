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

	"github.com/AliSohani2082/sneakernet/internal/links"
)

// errInputClosed means stdin ended while a question was open.
var errInputClosed = errors.New("input closed before the question was answered")

// ui writes progress and asks questions on a plain terminal. It works over
// serial consoles and pipes, so tests and VMs can script it.
type ui struct {
	in    *bufio.Reader
	out   io.Writer
	color bool
	yes   bool // accept every default without asking
	log   io.Writer
}

func newUI(in io.Reader, out io.Writer) *ui {
	u := &ui{in: bufio.NewReader(in), out: out, log: io.Discard}
	if f, ok := out.(*os.File); ok && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			u.color = true
		}
	}
	return u
}

func (u *ui) paint(code, s string) string {
	if !u.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (u *ui) bold(s string) string { return u.paint("1", s) }
func (u *ui) dim(s string) string  { return u.paint("2", s) }

func (u *ui) printf(format string, a ...any) {
	fmt.Fprintf(u.out, format, a...)
	fmt.Fprintf(u.log, format, a...)
}

func (u *ui) line(prefix, code, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	fmt.Fprintf(u.out, "%s %s\n", u.paint(code, prefix), msg)
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
