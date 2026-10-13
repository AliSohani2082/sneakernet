package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

// bannerArt is the wordmark. Seven-bit ASCII only, so it draws on the Linux
// console with any font; at most 54 columns, four lines.
const bannerArt = "                       _                          _\n" +
	" ___  _ _   ___  __ _ | |__  ___  _ _  _ _   ___ | |_\n" +
	"(_-< | ' \\ / -_)/ _` || / / / -_)| '_|| ' \\ / -_)|  _|\n" +
	"/__/ |_||_|\\___|\\__,_||_\\_\\ \\___||_|  |_||_|\\___| \\__|\n"

// bannerTagline sits under the art.
const bannerTagline = "offline Xray client - carry it on a stick"

// bannerCompact is for terminals narrower than bannerMinWidth.
const bannerCompact = "== sneakernet == offline Xray client"

// bannerMinWidth is the width below which the compact line is used.
const bannerMinWidth = 60

// renderBanner returns the banner for a terminal of the given width, with a
// trailing blank line. Color is applied only when color is true.
func renderBanner(color bool, width int) string {
	if width < bannerMinWidth {
		return paintIf(color, "1;36", bannerCompact) + "\n\n"
	}
	return paintIf(color, "1;36", bannerArt) + paintIf(color, "2", bannerTagline) + "\n\n"
}

// showBanner prints the banner to w when that is welcome: w is a terminal and
// the user did not ask for quiet, scripted output. Pipes, --no-input and
// --yes never see it.
func (u *ui) showBanner(w io.Writer) {
	width := 80
	if f, ok := w.(*os.File); ok {
		if cols, _, err := term.GetSize(f.Fd()); err == nil && cols > 0 {
			width = cols
		}
	}
	u.bannerTo(w, isTerminal(w), width)
}

// bannerTo is showBanner with the terminal facts given, for tests.
func (u *ui) bannerTo(w io.Writer, tty bool, width int) {
	if !tty || u.g.noInput || u.yes {
		return
	}
	fmt.Fprint(w, renderBanner(colorEnabled(u.g.color, os.Getenv, tty), width))
}

// bannerLines is for tests: the art without color.
func bannerLines() []string { return strings.Split(strings.TrimRight(bannerArt, "\n"), "\n") }
