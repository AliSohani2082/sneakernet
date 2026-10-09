package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// cmdDoc is the help text of a command, shown by "help <cmd>" and "<cmd> -h".
// The flags are listed from the command's own FlagSet.
type cmdDoc struct {
	name     string
	synopsis string
	summary  string
	examples []string
}

// docs lists the commands in the order the global help shows them.
var docs = []cmdDoc{
	{"install", "install [flags]", "install Xray and the preset servers (run from the USB stick)", []string{
		"sudo sh /path/to/stick/sneakernet/install.sh",
		"sudo sh /path/to/stick/sneakernet/install.sh --yes --server auto --routing bypass-region --region ir",
	}},
	{"tui", "tui", "terminal UI: switch servers, test them, watch logs (F1 inside shows the keys)", []string{
		"sudo sneakernet tui",
		"sudo sneakernet --ascii tui      plain ASCII symbols, for the Linux console",
	}},
	{"status", "status [--check]", "show the service, the active server and the proxy ports", []string{
		"sudo sneakernet status",
		"sudo sneakernet status --check   also fetch a page through the proxy",
	}},
	{"list", "list", "list the servers", []string{"sudo sneakernet list"}},
	{"add", "add [file...]", "add servers from files, or from stdin (pipe or paste, then Ctrl-D)", []string{
		"sudo sneakernet add links.txt",
		"echo 'vless://...' | sudo sneakernet add",
	}},
	{"remove", "remove <number>", "remove a server (numbers are in: sneakernet list)", []string{"sudo sneakernet remove 12"}},
	{"switch", "switch <number|auto>", "use another server", []string{
		"sudo sneakernet switch 12      use server #12 (see: sneakernet list)",
		"sudo sneakernet switch auto    keep testing all servers, use the fastest",
	}},
	{"test", "test [--all]", "check the connection (--all tests every server)", []string{
		"sudo sneakernet test",
		"sudo sneakernet test --all --timeout 5s",
	}},
	{"doctor", "doctor", "diagnose an installation", []string{"sudo sneakernet doctor"}},
	{"uninstall", "uninstall [--yes]", "remove everything sneakernet installed", []string{
		"sudo sneakernet uninstall",
		"sudo sneakernet uninstall --yes",
	}},
	{"convert", "convert --servers <file> [flags]", "print the Xray config for a server list (no install needed)", []string{
		"sneakernet convert --servers links.txt --server auto > config.json",
		"cat links.txt | sneakernet convert --server 2 --routing global",
	}},
	{"version", "version", "print the version", nil},
}

func docFor(name string) (cmdDoc, bool) {
	for _, d := range docs {
		if d.name == name {
			return d, true
		}
	}
	return cmdDoc{}, false
}

func commandNames() []string {
	names := make([]string, len(docs))
	for i, d := range docs {
		names[i] = d.name
	}
	return names
}

// usageText is the global help.
func usageText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "sneakernet %s — offline Xray client manager\n\n", version)
	b.WriteString("Usage: sneakernet [global flags] <command> [flags]\n\nCommands:\n")
	for _, d := range docs {
		fmt.Fprintf(&b, "  %-10s  %s\n", d.name, d.summary)
	}
	b.WriteString(`
Global flags (anywhere on the line):
  --color auto|always|never  colors; auto needs a terminal and no NO_COLOR (also: SNEAKERNET_COLOR)
  --no-color                 same as --color=never
  --no-input                 never ask questions; fail naming the flag to pass instead
  --ascii                    plain ASCII symbols (chosen automatically on the Linux console)

Output: results go to stdout; progress, warnings and errors go to stderr.

Exit codes:
  0    success
  1    the operation failed
  2    usage error: bad flag, wrong arguments, unknown command, a question with no terminal
  3    sneakernet is not installed here
  4    root is needed and sudo is not available
  5    no connectivity through the proxy (test, status --check, doctor)
  130  interrupted

Run "sneakernet help <command>" for a command's examples and flags.
`)
	return b.String()
}

// printHelp writes the help of one command to u.out.
func printHelp(ctx context.Context, d cmdDoc, cmd func(context.Context, []string, *ui) error, u *ui) {
	var b strings.Builder
	fmt.Fprintf(&b, "usage: sneakernet %s\n\n  %s\n", d.synopsis, d.summary)
	if len(d.examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, e := range d.examples {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	// The command lists its own flags when asked with -h.
	var buf bytes.Buffer
	cu := &ui{in: u.in, out: &buf, err: &buf, log: u.log, g: u.g}
	_ = cmd(ctx, []string{"-h"}, cu)
	if _, flags, ok := strings.Cut(buf.String(), "\n"); ok && strings.TrimSpace(flags) != "" {
		b.WriteString("\nFlags:\n" + flags)
	}
	b.WriteString("\nGlobal flags (--color, --no-color, --no-input, --ascii) and exit codes: sneakernet help\n")
	fmt.Fprint(u.out, b.String())
}
