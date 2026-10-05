// Command sneakernet installs and manages an offline Xray client.
//
// It ships on the Ventoy stick inside the bundle (bin/<arch>/sneakernet) and
// is copied to /opt/sneakernet/bin on install.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `sneakernet %s — offline Xray client manager

Usage: sneakernet <command> [flags]

Commands:
  install     install Xray and the preset servers (run from the USB stick)
  tui         terminal UI: switch servers, test them, watch logs
  status      show the service, the active server and the proxy ports
  list        list the preset servers
  switch      use another server:  sneakernet switch 12  |  sneakernet switch auto
  test        check the connection (--all tests every server)
  doctor      diagnose an installation
  uninstall   remove everything sneakernet installed
  convert     print the Xray config for a server list (no install needed)
  version     print the version

Run "sneakernet <command> -h" for the flags of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout))
}

func run(ctx context.Context, args []string, in io.Reader, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(out, usage, version)
		return 2
	}
	u := newUI(in, out)
	cmds := map[string]func(context.Context, []string, *ui) error{
		"install":   cmdInstall,
		"tui":       cmdTUI,
		"status":    cmdStatus,
		"list":      cmdList,
		"switch":    cmdSwitch,
		"test":      cmdTest,
		"doctor":    cmdDoctor,
		"uninstall": cmdUninstall,
		"convert":   cmdConvert,
		"version": func(context.Context, []string, *ui) error {
			fmt.Fprintf(out, "sneakernet %s (%s)\n", version, detect.Arch())
			return nil
		},
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprintf(out, usage, version)
		return 0
	}
	cmd, ok := cmds[args[0]]
	if !ok {
		fmt.Fprintf(out, "unknown command %q\n\n"+usage, args[0], version)
		return 2
	}
	if err := cmd(ctx, args[1:], u); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		if errors.Is(err, errReexec) {
			return 0
		}
		var ee *exitError
		if errors.As(err, &ee) {
			return ee.code
		}
		u.fail("%v", err)
		return 1
	}
	return 0
}

// exitError ends the program with a code after the message was printed.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

var errReexec = errors.New("re-executed with sudo")

// env is an installation on a target.
type env struct {
	t    target.Target
	init string
	mgr  *manage.Manager
}

// openEnv returns the installation in root ("" = the running system).
func openEnv(root string) *env {
	t := target.RunningSystem()
	if root != "" {
		t = target.Dir(root)
	}
	e := &env{t: t, init: detect.InitSystem(t.Root, t.Running)}
	e.mgr = &manage.Manager{T: t}
	if svc, err := service.For(t, e.init); err == nil {
		e.mgr.Svc = svc
	}
	return e
}

// requireRoot re-runs the command with sudo when it needs root. With a
// --root test directory no privileges are needed.
func requireRoot(u *ui, root string) error {
	if root != "" || os.Geteuid() == 0 {
		return nil
	}
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return errors.New("this command needs root; run it as root")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	u.printf("%s\n", u.dim("needs root — running it with sudo"))
	cmd := exec.Command(sudo, append([]string{self}, os.Args[1:]...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var xe *exec.ExitError
		if errors.As(err, &xe) {
			return &exitError{code: xe.ExitCode()}
		}
		return err
	}
	return errReexec
}
