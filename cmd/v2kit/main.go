// Command v2kit is the offline installer, manager and TUI for v2ray-kit.
package main

import (
	"fmt"
	"os"
)

const usage = `usage: v2kit <command> [flags]

commands:
  install     install xray (+ optional GUI/TUI) into a target system
  uninstall   remove everything listed in the install manifest
  tui         interactive terminal UI (switch server, status, logs)
  doctor      diagnose an existing installation
  convert     convert share links to an xray config (stdout)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "install", "uninstall", "tui", "doctor", "convert":
		fmt.Fprintf(os.Stderr, "v2kit %s: not implemented yet (milestone M1)\n", os.Args[1])
		os.Exit(1)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
