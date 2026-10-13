package tui

import (
	"strings"

	"charm.land/bubbles/v2/spinner"
)

// glyphs is the set of symbols the UI draws with. The Linux console keeps at
// most 512 glyphs, so braille spinner frames, emoji flags and most arrows are
// not available there; ttyGlyphs only uses printable ASCII.
type glyphs struct {
	ascii            bool
	cursor           string // two cells
	active, inactive string
	sep              string // between short-help items
	dot              string // inside a line of facts
	rule             string // horizontal lines; box drawing is not in every font or locale
	ellipsis         string
	up, down         string
	spinner          spinner.Spinner
}

var (
	unicodeGlyphs = glyphs{
		cursor: "› ", active: "●", inactive: "○", sep: " • ", dot: "·", rule: "─", ellipsis: "…",
		up: "↑", down: "↓", spinner: spinner.MiniDot,
	}
	ttyGlyphs = glyphs{
		ascii:  true,
		cursor: "> ", active: "*", inactive: "o", sep: " | ", dot: "|", rule: "-", ellipsis: "...",
		up: "up", down: "down", spinner: spinner.Line,
	}
)

// DetectASCII reports whether the environment calls for ASCII-only output:
// SNEAKERNET_ASCII is set, the terminal is the Linux console, or the locale is
// set to something other than UTF-8. A completely unset locale is not enough,
// because sudo drops LANG and that would turn ASCII on in a UTF-8 terminal;
// the CLI passes --ascii through its sudo re-exec instead.
func DetectASCII(env func(string) string) bool {
	if env("SNEAKERNET_ASCII") != "" || env("TERM") == "linux" {
		return true
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := env(k); v != "" {
			u := strings.ToUpper(v)
			return !strings.Contains(u, "UTF-8") && !strings.Contains(u, "UTF8")
		}
	}
	return false
}

func pickGlyphs(ascii bool) glyphs {
	if ascii {
		return ttyGlyphs
	}
	return unicodeGlyphs
}

var asciiReplacer = strings.NewReplacer(
	"—", "-", "–", "-", "…", "...", "·", "|", "›", ">", "●", "*", "○", "o",
	"•", "|", "↑", "^", "↓", "v", "“", `"`, "”", `"`, "’", "'",
)

func isRegional(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// isPictograph covers emoji and symbols the console font has no glyph for.
func isPictograph(r rune) bool {
	return (r >= 0x1F300 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) ||
		(r >= 0x2B00 && r <= 0x2BFF) || (r >= 0x2300 && r <= 0x23FF && r != 0x2318)
}

// isInvisible are the joiners and selectors that only modify emoji.
func isInvisible(r rune) bool {
	return r == 0xFE0F || r == 0xFE0E || r == 0x200D || r == 0x20E3 || (r >= 0xE0020 && r <= 0xE007F)
}

// asciiName turns a server name into something the console can draw and maps
// the indexes of matched runes (for highlighting) onto the result: a flag
// pair becomes "[DE]", other pictographs become "?", joiners disappear.
func asciiName(name string, matches []int) (string, []int) {
	hit := make(map[int]bool, len(matches))
	for _, i := range matches {
		hit[i] = true
	}
	src := []rune(name)
	var out []rune
	var outMatches []int
	emit := func(matched bool, rs ...rune) {
		for _, r := range rs {
			if matched {
				outMatches = append(outMatches, len(out))
			}
			out = append(out, r)
		}
	}
	for i := 0; i < len(src); i++ {
		r := src[i]
		switch {
		case isRegional(r) && i+1 < len(src) && isRegional(src[i+1]):
			emit(hit[i] || hit[i+1], '[', 'A'+(r-0x1F1E6), 'A'+(src[i+1]-0x1F1E6), ']')
			i++
		case isInvisible(r):
		case isPictograph(r) || isRegional(r):
			emit(hit[i], '?')
		default:
			emit(hit[i], r)
		}
	}
	return string(out), outMatches
}

// safe makes free text drawable in ASCII mode; it is the identity otherwise.
func (g glyphs) safe(s string) string {
	if !g.ascii {
		return s
	}
	s, _ = asciiName(asciiReplacer.Replace(s), nil)
	return s
}
