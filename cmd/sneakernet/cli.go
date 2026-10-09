package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/xrayconf"
)

// Exit codes. They are part of the interface: scripts branch on them.
const (
	exitOK           = 0
	exitFailed       = 1   // the operation failed
	exitUsage        = 2   // bad flag, wrong argument count, unknown command
	exitNotInstalled = 3   // sneakernet is not installed here
	exitNeedRoot     = 4   // root is needed and sudo is not available
	exitNoNet        = 5   // no connectivity through the proxy
	exitInterrupted  = 130 // Ctrl-C or SIGTERM
)

// codedError is an error with its own exit code. run prints the message.
type codedError struct {
	code int
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

func coded(code int, format string, a ...any) error {
	return &codedError{code, fmt.Errorf(format, a...)}
}

func usageErr(format string, a ...any) error { return coded(exitUsage, format, a...) }

// colorMode is the --color flag.
type colorMode string

const (
	colorAuto   colorMode = "auto"
	colorAlways colorMode = "always"
	colorNever  colorMode = "never"
)

// globals are the flags every command accepts, anywhere on the line.
type globals struct {
	color   colorMode
	noInput bool
	ascii   bool
}

// parseGlobals removes the global flags from args. Flags after "--" are left
// alone. SNEAKERNET_COLOR sets the default color mode.
func parseGlobals(args []string, getenv func(string) string) ([]string, globals, error) {
	g := globals{color: colorAuto}
	if v := getenv("SNEAKERNET_COLOR"); v != "" {
		m, err := parseColorMode(v)
		if err != nil {
			return nil, g, fmt.Errorf("SNEAKERNET_COLOR: %w", err)
		}
		g.color = m
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(rest, args[i:]...), g, nil
		case a == "--no-color" || a == "-no-color":
			g.color = colorNever
		case a == "--no-input" || a == "-no-input":
			g.noInput = true
		case a == "--ascii" || a == "-ascii":
			g.ascii = true
		case a == "--color" || a == "-color":
			if i+1 >= len(args) {
				return nil, g, errors.New("--color needs a value: auto, always or never")
			}
			i++
			m, err := parseColorMode(args[i])
			if err != nil {
				return nil, g, err
			}
			g.color = m
		case strings.HasPrefix(a, "--color=") || strings.HasPrefix(a, "-color="):
			m, err := parseColorMode(a[strings.IndexByte(a, '=')+1:])
			if err != nil {
				return nil, g, err
			}
			g.color = m
		default:
			rest = append(rest, a)
		}
	}
	return rest, g, nil
}

func parseColorMode(s string) (colorMode, error) {
	switch m := colorMode(strings.ToLower(s)); m {
	case colorAuto, colorAlways, colorNever:
		return m, nil
	}
	return "", fmt.Errorf("invalid --color value %q: use auto, always or never", s)
}

// colorEnabled is the one rule for the CLI and the TUI. An explicit
// --color wins; otherwise colors need a terminal, TERM other than dumb, and
// NO_COLOR unset or empty (no-color.org).
func colorEnabled(mode colorMode, getenv func(string) string, tty bool) bool {
	switch mode {
	case colorAlways:
		return true
	case colorNever:
		return false
	}
	return tty && getenv("NO_COLOR") == "" && getenv("TERM") != "dumb"
}

// reexecGlobals are the global flags to repeat when the command is re-run
// under sudo, which drops NO_COLOR and SNEAKERNET_COLOR from the environment
// (and cannot see a decision that came from the parent's own checks).
func (u *ui) reexecGlobals() []string {
	var out []string
	switch {
	case u.g.color == colorAlways:
		out = append(out, "--color=always")
	case u.g.color == colorNever || os.Getenv("NO_COLOR") != "":
		out = append(out, "--color=never")
	}
	if u.g.noInput {
		out = append(out, "--no-input")
	}
	if u.ascii {
		out = append(out, "--ascii")
	}
	return out
}

// isFlagError recognises the errors the flag package returns for bad
// command lines. It has already printed them with the usage.
func isFlagError(err error) bool {
	msg := err.Error()
	for _, p := range []string{"flag provided but not defined", "flag needs an argument",
		"bad flag syntax", "invalid value ", "invalid boolean"} {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}

// suggest returns the known names closest to s, for "did you mean".
func suggest(s string, names []string) []string {
	type cand struct {
		name string
		dist int
	}
	var cs []cand
	for _, n := range names {
		d := levenshtein(s, n)
		if strings.HasPrefix(n, s) && len(s) >= 2 {
			d = 1
		}
		if d <= 2 && d < len(n) {
			cs = append(cs, cand{n, d})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].dist < cs[j].dist })
	var out []string
	for _, c := range cs {
		out = append(out, c.name)
	}
	return out
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// reinstallCommand is the install command line that gives the same setup
// without questions. args is the installer's own command line: its --bundle
// says where the stick's install.sh is.
func reinstallCommand(args []string, st manage.State) string {
	def := manage.DefaultState()
	parts := []string{"sudo"}
	if b := flagValue(args, "bundle"); b != "" {
		parts = append(parts, "sh", shellQuote(filepath.Join(b, "install.sh")))
	} else {
		parts = append(parts, "sneakernet", "install")
	}
	parts = append(parts, "--yes", "--server", selectionArg(st), "--routing", st.Routing)
	if st.Routing == xrayconf.RoutingBypassRegion && st.Region != "" {
		parts = append(parts, "--region", st.Region)
	}
	if st.SocksPort != def.SocksPort {
		parts = append(parts, "--socks-port", strconv.Itoa(st.SocksPort))
	}
	if st.HTTPPort != def.HTTPPort {
		parts = append(parts, "--http-port", strconv.Itoa(st.HTTPPort))
	}
	return strings.Join(parts, " ")
}

// flagValue finds "--name value" or "--name=value" in args.
func flagValue(args []string, name string) string {
	for i, a := range args {
		a = strings.TrimLeft(a, "-")
		switch {
		case a == name && i+1 < len(args) && strings.HasPrefix(args[i], "-"):
			return args[i+1]
		case strings.HasPrefix(a, name+"=") && strings.HasPrefix(args[i], "-"):
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

// shellQuote leaves plain words alone and single-quotes anything else.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:=@%+,") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
