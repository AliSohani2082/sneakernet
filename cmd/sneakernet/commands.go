package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/AliSohani2082/sneakernet/internal/install"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/probe"
	"github.com/AliSohani2082/sneakernet/internal/tui"
	"github.com/AliSohani2082/sneakernet/internal/xrayconf"
)

// installed opens the installation, re-running with sudo when needed.
func installed(u *ui, root string) (*env, error) {
	if err := requireRoot(u, root); err != nil {
		return nil, err
	}
	e := openEnv(root)
	if !e.mgr.Installed() {
		return nil, errors.New("sneakernet is not installed here; run install.sh from the USB stick")
	}
	return e, nil
}

func rootFlag(fs *flag.FlagSet) *string {
	return fs.String("root", "", "operate on an installation in this directory (testing)")
}

func newFlags(name string, u *ui) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(u.out)
	return fs
}

func cmdStatus(ctx context.Context, args []string, u *ui) error {
	fs := newFlags("status", u)
	root := rootFlag(fs)
	check := fs.Bool("check", false, "also test the connection")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := installed(u, *root)
	if err != nil {
		return err
	}
	st, err := e.mgr.State()
	if err != nil {
		return err
	}
	ss, err := e.mgr.Status()
	if err != nil {
		return err
	}
	state := ss.State
	if ss.Active {
		state = u.paint("32", state)
	} else {
		state = u.paint("31", state)
	}
	u.printf("Service   %s  %s\n", state, u.dim(ss.Since))
	u.printf("Server    %s\n", st.Describe())
	u.printf("Routing   %s\n", routingLabel(st))
	u.printf("Proxies   SOCKS5 127.0.0.1:%d   HTTP 127.0.0.1:%d\n", st.SocksPort, st.HTTPPort)
	if *check {
		r := probe.Through(ctx, fmt.Sprintf("127.0.0.1:%d", st.SocksPort), "", 15*time.Second)
		u.printf("Internet  %s\n", r)
	}
	return nil
}

func cmdList(_ context.Context, args []string, u *ui) error {
	fs := newFlags("list", u)
	root := rootFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := installed(u, *root)
	if err != nil {
		return err
	}
	servers, lineErrs, err := e.mgr.Servers()
	if err != nil {
		return err
	}
	st, _ := e.mgr.State()
	mark := 0
	if !st.Auto {
		mark = st.Index
	}
	u.serverTable(servers, mark)
	for _, le := range lineErrs {
		u.warn("%s: skipped: %v", layout.ServersFile, le)
	}
	return nil
}

func cmdSwitch(ctx context.Context, args []string, u *ui) error {
	fs := newFlags("switch", u)
	root := rootFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: sneakernet switch <number|auto>")
	}
	e, err := installed(u, *root)
	if err != nil {
		return err
	}
	servers, _, err := e.mgr.Servers()
	if err != nil {
		return err
	}
	st, _ := e.mgr.State()
	if err := chooseServer(u, servers, fs.Arg(0), &st); err != nil {
		return err
	}
	st, err = e.mgr.Switch(ctx, st)
	if err != nil {
		return err
	}
	u.ok("now using %s", st.Describe())
	if e.t.Running {
		checkConnection(ctx, u, st)
	}
	return nil
}

func cmdTest(ctx context.Context, args []string, u *ui) error {
	fs := newFlags("test", u)
	root := rootFlag(fs)
	all := fs.Bool("all", false, "test every server, not just the active one")
	url := fs.String("url", xrayconf.DefaultProbeURL, "URL to fetch through the proxy")
	timeout := fs.Duration("timeout", 10*time.Second, "per-server timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := installed(u, *root)
	if err != nil {
		return err
	}
	if !*all {
		st, err := e.mgr.State()
		if err != nil {
			return err
		}
		r := probe.Through(ctx, fmt.Sprintf("127.0.0.1:%d", st.SocksPort), *url, *timeout)
		if !r.OK() {
			u.fail("%s: %v", st.Describe(), r.Err)
			return &exitError{code: 1}
		}
		u.ok("%s: %s", st.Describe(), r)
		return nil
	}
	servers, _, err := e.mgr.Servers()
	if err != nil {
		return err
	}
	u.step("Testing %d servers (up to %s each)", countUsable(servers), *timeout)
	results, err := probe.All(ctx, e.t.Path(layout.XrayBin), e.t.Path(layout.AssetDir), servers, *url, *timeout)
	if err != nil {
		return err
	}
	printResults(u, servers, results)
	return nil
}

func countUsable(servers []links.Server) int {
	n := 0
	for _, s := range servers {
		if s.Usable() {
			n++
		}
	}
	return n
}

// printResults lists working servers fastest first, then the failures.
func printResults(u *ui, servers []links.Server, results map[int]probe.Result) {
	sorted := append([]links.Server(nil), servers...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, aok := results[sorted[i].Index]
		b, bok := results[sorted[j].Index]
		switch {
		case aok && a.OK() && bok && b.OK():
			return a.Latency < b.Latency
		case aok && a.OK():
			return true
		default:
			return false
		}
	})
	working := 0
	for _, s := range sorted {
		r, ok := results[s.Index]
		if !ok {
			continue
		}
		if r.OK() {
			working++
			u.printf("  %s %4d  %-40s %6d ms\n", u.paint("32", "ok"), s.Index, clip(s.Name, 40), r.Latency.Milliseconds())
		} else {
			u.printf("%s\n", u.dim(fmt.Sprintf("  -- %4d  %-40s %s", s.Index, clip(s.Name, 40), r.Err)))
		}
	}
	u.printf("\n%d of %d servers work right now. Use one with: sudo sneakernet switch <number>\n", working, len(results))
}

func cmdDoctor(ctx context.Context, args []string, u *ui) error {
	fs := newFlags("doctor", u)
	root := rootFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireRoot(u, *root); err != nil {
		return err
	}
	e := openEnv(*root)
	problems := 0
	check := func(name string, err error) {
		if err != nil {
			problems++
			u.fail("%s: %v", name, err)
		} else {
			u.ok("%s", name)
		}
	}
	for _, p := range []string{layout.XrayBin, layout.ServersFile, layout.ConfigFile, layout.StateFile,
		layout.AssetDir + "/geoip.dat", layout.AssetDir + "/geosite.dat"} {
		_, err := os.Stat(e.t.Path(p))
		check(p, err)
	}
	st, err := e.mgr.State()
	check("state", err)
	if cfg, err := os.ReadFile(e.t.Path(layout.ConfigFile)); err == nil {
		check("Xray accepts the config",
			xrayconf.Validate(ctx, e.t.Path(layout.XrayBin), e.t.Path(layout.AssetDir), cfg))
	}
	if e.mgr.Svc == nil {
		check("service manager", fmt.Errorf("%s is not supported", e.init))
	} else if ss, err := e.mgr.Status(); err != nil {
		check("service", err)
	} else if e.t.Running {
		if !ss.Active {
			err = fmt.Errorf("%s; logs: journalctl -u %s", ss.State, layout.UnitName)
		}
		check("service running", err)
		if !ss.Enabled {
			check("service enabled at boot", errors.New("disabled"))
		}
	}
	if e.t.Running {
		addr := fmt.Sprintf("127.0.0.1:%d", st.SocksPort)
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			c.Close()
		}
		check("SOCKS port "+addr+" listening", err)
		if err == nil {
			r := probe.Through(ctx, addr, "", 15*time.Second)
			check("internet through the proxy", r.Err)
		}
	}
	if problems > 0 {
		return &exitError{code: 1}
	}
	return nil
}

func cmdUninstall(_ context.Context, args []string, u *ui) error {
	fs := newFlags("uninstall", u)
	root := rootFlag(fs)
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := requireRoot(u, *root); err != nil {
		return err
	}
	e := openEnv(*root)
	if !*yes {
		ok, err := u.confirm(fmt.Sprintf("Remove Sneakernet, its service and %s?", strings.Join(
			[]string{layout.OptDir, layout.EtcDir, layout.VarDir}, ", ")), false)
		if err != nil || !ok {
			return err
		}
	}
	if err := install.Uninstall(e.t, e.mgr.Svc, func(f string, a ...any) { u.printf("    "+f+"\n", a...) }); err != nil {
		return err
	}
	u.ok("Sneakernet is removed")
	return nil
}

func cmdConvert(_ context.Context, args []string, u *ui) error {
	fs := newFlags("convert", u)
	servers := fs.String("servers", "", "file with share links (default: stdin)")
	sel := fs.String("server", "auto", `"auto" or a server number`)
	routing := fs.String("routing", xrayconf.RoutingBypassLAN, "routing preset: "+strings.Join(xrayconf.RoutingPresets, ", "))
	region := fs.String("region", "", "country code for bypass-region")
	if err := fs.Parse(args); err != nil {
		return err
	}
	in := u.in
	var list []links.Server
	var err error
	if *servers != "" {
		list, _, err = readServers(*servers)
	} else {
		list, _, err = links.ParseList(in)
	}
	if err != nil {
		return err
	}
	st := manage.DefaultState()
	if err := chooseServer(u, list, *sel, &st); err != nil {
		return err
	}
	st.Routing, st.Region = *routing, *region
	cfg, err := xrayconf.Build(list, st.Selection(), st.Options())
	if err != nil {
		return err
	}
	b, err := cfg.JSON()
	if err != nil {
		return err
	}
	_, err = u.out.Write(b)
	return err
}

func cmdTUI(ctx context.Context, args []string, u *ui) error {
	fs := newFlags("tui", u)
	root := rootFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	e, err := installed(u, *root)
	if err != nil {
		return err
	}
	return tui.Run(ctx, e.mgr)
}
