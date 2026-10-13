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

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes one command line and returns the exit code (see cli.go).
func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) int {
	rest, g, gerr := parseGlobals(args, os.Getenv)
	u := newUI(in, out, errOut, g)
	if gerr != nil {
		u.fail("%v", gerr)
		return exitUsage
	}
	u.args = rest
	if len(rest) == 0 {
		u.showBanner(errOut)
		fmt.Fprint(errOut, usageText())
		return exitUsage
	}
	cmds := map[string]func(context.Context, []string, *ui) error{
		"install":   cmdInstall,
		"tui":       cmdTUI,
		"status":    cmdStatus,
		"list":      cmdList,
		"switch":    cmdSwitch,
		"add":       cmdAdd,
		"remove":    cmdRemove,
		"test":      cmdTest,
		"doctor":    cmdDoctor,
		"uninstall": cmdUninstall,
		"convert":   cmdConvert,
		"version":   cmdVersion,
	}
	name := rest[0]
	switch name {
	case "-h", "-help", "--help":
		u.showBanner(out)
		fmt.Fprint(out, usageText())
		return exitOK
	case "help":
		if len(rest) == 1 {
			u.showBanner(out)
			fmt.Fprint(out, usageText())
			return exitOK
		}
		name = rest[1]
		cmd, ok := cmds[name]
		if !ok {
			return unknownCommand(u, name)
		}
		d, _ := docFor(name)
		printHelp(ctx, d, cmd, u)
		return exitOK
	}
	cmd, ok := cmds[name]
	if !ok {
		return unknownCommand(u, name)
	}
	for _, a := range rest[1:] {
		if a == "--" {
			break
		}
		if a == "-h" || a == "-help" || a == "--help" {
			d, _ := docFor(name)
			printHelp(ctx, d, cmd, u)
			return exitOK
		}
	}
	err := cmd(ctx, rest[1:], u)
	if err == nil {
		return exitOK
	}
	var ee *exitError
	var ce *codedError
	switch {
	case errors.Is(err, flag.ErrHelp), errors.Is(err, errReexec):
		return exitOK
	case errors.As(err, &ee):
		return ee.code // the message was printed already
	case isFlagError(err):
		return exitUsage // the flag package printed it with the usage
	case errors.As(err, &ce):
		u.fail("%v", ce.err)
		return ce.code
	case ctx.Err() != nil:
		u.fail("interrupted")
		return exitInterrupted
	}
	u.fail("%v", err)
	return exitFailed
}

func unknownCommand(u *ui, name string) int {
	u.fail("unknown command %q", name)
	if s := suggest(name, commandNames()); len(s) > 0 {
		fmt.Fprintf(u.err, "    Did you mean %q?\n", s[0])
	}
	fmt.Fprintln(u.err, "    Run \"sneakernet help\" for the list of commands.")
	return exitUsage
}

func cmdVersion(_ context.Context, args []string, u *ui) error {
	fs := newFlags("version", u)
	if err := fs.Parse(args); err != nil {
		return err
	}
	u.showBanner(u.out)
	u.printf("sneakernet %s (%s)\n", version, detect.Arch())
	return nil
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
// --root test directory no privileges are needed. sudo resets the
// environment, so the global flags that depend on it travel as arguments.
func requireRoot(u *ui, root string) error {
	if root != "" || os.Geteuid() == 0 {
		return nil
	}
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return coded(exitNeedRoot, "this command needs root; run it as root")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	u.notef("needs root — running it with sudo")
	args := append(append([]string{self}, u.reexecGlobals()...), u.args...)
	cmd := exec.Command(sudo, args...)
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
