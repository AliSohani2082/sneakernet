package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/AliSohani2082/sneakernet/internal/bundle"
	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/install"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/probe"
	"github.com/AliSohani2082/sneakernet/internal/xrayconf"
)

func cmdInstall(ctx context.Context, args []string, u *ui) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(u.out)
	bundleDir := fs.String("bundle", "", "bundle directory (default: the folder this binary came from)")
	arch := fs.String("arch", detect.Arch(), "CPU architecture of the binaries to install")
	where := fs.String("target", "", `where to install: "/" for this running system`)
	root := fs.String("root", "", "install into this directory instead of the running system (testing)")
	server := fs.String("server", "", `server to use: "auto" or a number from the list`)
	routing := fs.String("routing", "", "routing preset: "+strings.Join(xrayconf.RoutingPresets, ", "))
	region := fs.String("region", "", "two-letter country code for bypass-region, e.g. ir")
	socksPort := fs.Int("socks-port", 0, "local SOCKS5 port (default 10808)")
	httpPort := fs.Int("http-port", 0, "local HTTP proxy port (default 10809)")
	yes := fs.Bool("yes", false, "accept the defaults; ask nothing")
	skipVerify := fs.Bool("skip-verify", false, "do not check SHA256SUMS (development only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	u.yes = *yes
	if err := requireRoot(u, *root); err != nil {
		return err
	}

	u.printf("\n%s %s — offline Xray installer\n", u.bold("sneakernet"), version)

	// 1. Bundle
	dir := *bundleDir
	if dir == "" {
		dir = defaultBundleDir()
	}
	b, err := bundle.Open(dir, *arch)
	if err != nil {
		return err
	}
	if *skipVerify {
		u.warn("skipping the bundle checksum check")
	} else {
		u.step("Checking the files on the stick")
		if err := b.Verify(); err != nil {
			return err
		}
		u.ok("bundle %s is intact", b.Version())
	}

	// 2. Where
	e, err := chooseTarget(u, *where, *root)
	if err != nil {
		return err
	}
	if *root == "" {
		if f, err := os.OpenFile(layout.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			defer f.Close()
			u.log = f
			fmt.Fprintf(f, "\n--- sneakernet %s install, %s\n", version, time.Now().Format(time.RFC3339))
		}
	}
	host, _ := detect.ReadOS(e.t.Root)
	u.ok("%s · %s · init: %s", host.PrettyName, *arch, e.init)
	live := e.t.Running && detect.LiveSession()
	if live {
		u.warn("this is a live session: the install is lost on reboot unless Ventoy persistence is set up")
	}
	if e.mgr.Svc == nil {
		u.warn("%s is not supported yet; Xray will be installed but you start it yourself", e.init)
	}

	// 3. Interface
	if _, err := u.choose("Which interface?", []option{
		{label: "Terminal UI", note: "sneakernet tui"},
		{label: "Graphical app (v2rayN)", disabled: "coming in the next release"},
	}, 0); err != nil {
		return err
	}

	// 4. Servers: the stick's list, merged into what is already installed so
	// a re-install keeps servers added later in the TUI.
	data, err := serverListFor(e, b.ServersFile())
	if err != nil {
		return err
	}
	servers, lineErrs, _ := links.ParseList(bytes.NewReader(data))
	summarizeServers(u, servers, lineErrs)
	if countUsable(servers) == 0 {
		u.warn("no usable servers: %s on the stick is missing or has no working links", "servers.txt")
		if !*yes {
			if data, err = pasteLinks(u, data); err != nil {
				return err
			}
			servers, _, _ = links.ParseList(bytes.NewReader(data))
		}
	}

	prev, prevErr := e.mgr.State()
	st := manage.DefaultState()
	if prevErr == nil {
		st = prev
		if !*yes && *server == "" && *routing == "" {
			keep, err := u.confirm(fmt.Sprintf("\nSneakernet is already installed (server %s, routing %s). Keep these settings?",
				prev.Describe(), prev.Routing), true)
			if err != nil {
				return err
			}
			if keep {
				*server, *routing, *region = selectionArg(prev), prev.Routing, prev.Region
			}
		}
	}
	if *socksPort != 0 {
		st.SocksPort = *socksPort
	}
	if *httpPort != 0 {
		st.HTTPPort = *httpPort
	}
	if countUsable(servers) == 0 {
		u.warn("continuing without servers — add them later with: sudo sneakernet tui")
	} else if err := chooseServer(u, servers, *server, &st); err != nil {
		return err
	}
	if err := chooseRouting(u, *routing, *region, &st); err != nil {
		return err
	}
	// On a re-install our own Xray holds the ports, so only check fresh installs.
	if e.t.Running && prevErr != nil {
		for _, p := range []int{st.SocksPort, st.HTTPPort} {
			if portBusy(p) {
				return fmt.Errorf("port %d is already in use (another proxy running?); pick others with --socks-port/--http-port", p)
			}
		}
	}

	// 5. Install
	u.step("Installing")
	res, err := install.Install(ctx, install.Options{
		Bundle: b, Target: e.t, Svc: e.mgr.Svc, State: st, Servers: data, Log: u.logf,
	})
	if err != nil {
		return err
	}
	switch {
	case res.NoServers:
		u.ok("installed to %s", layout.OptDir)
		u.warn("the proxy is off until you add servers: sudo %s tui", res.Command)
		return nil
	case res.Started && res.BootOnly:
		u.ok("installed to %s, config checked by Xray", layout.OptDir)
		u.ok("service %s is running", layout.UnitName)
		u.warn("%s is read-only here (NixOS?), so the service is set up for this boot only", layout.UnitDir)
	case res.Started:
		u.ok("installed to %s, config checked by Xray", layout.OptDir)
		u.ok("service %s is running and starts at boot", layout.UnitName)
	case res.Enabled:
		u.ok("installed to %s, config checked by Xray", layout.OptDir)
		u.ok("service %s will start at boot", layout.UnitName)
	default:
		u.warn("start Xray with:\n      %s", res.Manual)
	}

	// 6. Check
	connected := false
	if res.Started {
		u.step("Testing the connection")
		connected = checkConnection(ctx, u, res.State)
	}
	printSummary(u, res.State, res.Command, connected, live)
	return nil
}

// defaultBundleDir is the bundle root when this binary runs from
// <bundle>/bin/<arch>/sneakernet, else the current directory.
func defaultBundleDir() string {
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if _, err := os.Stat(filepath.Join(d, bundle.SumsFile)); err == nil {
			return d
		}
	}
	return "."
}

func chooseTarget(u *ui, where, root string) (*env, error) {
	if root != "" {
		return openEnv(root), nil
	}
	switch where {
	case "/":
		return openEnv(""), nil
	case "":
	default:
		return nil, fmt.Errorf("installing into %s (an installed system on disk) is not supported yet; use --target /", where)
	}
	if _, err := u.choose("Where should Sneakernet be installed?", []option{
		{label: "This running system", note: "the live session or the OS you booted"},
		{label: "An installed system on disk", disabled: "coming in a later release"},
	}, 0); err != nil {
		return nil, err
	}
	return openEnv(""), nil
}

// serverListFor returns the server list to install: the installed list (on a
// re-install) followed by the stick's links that are not in it yet. A missing
// stick list is fine.
func serverListFor(e *env, stickList string) ([]byte, error) {
	stick, err := os.ReadFile(stickList)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	installed, err := os.ReadFile(e.t.Path(layout.ServersFile))
	if errors.Is(err, os.ErrNotExist) {
		return stick, nil
	}
	if err != nil {
		return nil, err
	}
	plan, err := e.mgr.PlanAdd(string(stick))
	if err != nil {
		return nil, err
	}
	out := bytes.TrimRight(installed, "\n")
	if len(out) > 0 {
		out = append(out, '\n')
	}
	for _, s := range plan.New {
		out = append(out, s.Raw+"\n"...)
	}
	return out, nil
}

// pasteLinks lets the user paste share links line by line, ending with an
// empty line. Pasting several lines at once works too.
func pasteLinks(u *ui, data []byte) ([]byte, error) {
	u.printf("\n%s\n", u.bold("Paste share links now"))
	u.printf("  one per line (vless://, vmess://, trojan://, ss://, hysteria2://);\n")
	u.printf("  press Enter on an empty line when done — or right away to add them later.\n")
	for {
		line, err := u.readLine()
		if err != nil || line == "" {
			return data, nil // EOF just ends the list
		}
		s, perr := links.Parse(line)
		switch {
		case perr != nil:
			u.warn("not a share link: %v", perr)
			continue
		case !s.Usable():
			u.warn("%s: %s (skipped)", clip(s.Name, 40), s.Problem)
			continue
		}
		u.ok("%s  %s", clip(s.Name, 40), u.dim(s.Kind()))
		if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
			data = append(data, '\n')
		}
		data = append(data, s.Raw+"\n"...)
	}
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

func readServers(path string) ([]links.Server, []links.LineError, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("server list: %w", err)
	}
	defer f.Close()
	return links.ParseList(f)
}

func summarizeServers(u *ui, servers []links.Server, lineErrs []links.LineError) {
	usable := 0
	reasons := map[string]int{}
	for _, s := range servers {
		if s.Usable() {
			usable++
		} else {
			reasons[s.Problem]++
		}
	}
	for _, e := range lineErrs {
		reasons["unreadable line: "+e.Err.Error()]++
	}
	u.ok("%d servers in the list, %d usable", len(servers), usable)
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return reasons[keys[i]] > reasons[keys[j]] })
	for _, k := range keys {
		u.printf("     %s\n", u.dim(fmt.Sprintf("%d skipped: %s", reasons[k], k)))
	}
}

func selectionArg(st manage.State) string {
	if st.Auto {
		return "auto"
	}
	return strconv.Itoa(st.Index)
}

func chooseServer(u *ui, servers []links.Server, arg string, st *manage.State) error {
	pick := func(a string) (bool, error) {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "auto" || a == "a" {
			st.UseAuto()
			return true, nil
		}
		n, err := strconv.Atoi(strings.TrimPrefix(a, "#"))
		if err != nil {
			return false, fmt.Errorf("%q is not a server number", a)
		}
		for i := range servers {
			if s := &servers[i]; s.Index == n {
				if !s.Usable() {
					return false, fmt.Errorf("server #%d cannot be used: %s", n, s.Problem)
				}
				st.Use(s)
				return true, nil
			}
		}
		return false, fmt.Errorf("there is no server #%d", n)
	}
	if arg != "" {
		_, err := pick(arg)
		return err
	}
	u.printf("\n%s\n", u.bold("Which server?"))
	u.printf("  auto — Xray keeps testing all servers and uses the fastest working one\n")
	u.printf("  a number — always use that server (type l to see the list)\n")
	def := selectionArg(*st)
	for {
		a, err := u.ask("Server", def)
		if err != nil {
			return err
		}
		if strings.EqualFold(a, "l") {
			u.serverTable(servers, st.Index)
			continue
		}
		if ok, err := pick(a); ok {
			return nil
		} else {
			u.warn("%v", err)
		}
	}
}

func chooseRouting(u *ui, routing, region string, st *manage.State) error {
	if routing != "" {
		st.Routing = routing
		if region != "" {
			st.Region = region
		}
		if st.Routing == xrayconf.RoutingBypassRegion && len(st.Region) != 2 {
			return errors.New("--routing bypass-region needs --region (a two-letter country code)")
		}
		return nil
	}
	presets := []string{xrayconf.RoutingBypassLAN, xrayconf.RoutingBypassRegion, xrayconf.RoutingGlobal}
	def := 0
	for i, p := range presets {
		if p == st.Routing {
			def = i
		}
	}
	i, err := u.choose("Which traffic should use the proxy?", []option{
		{label: "Everything except the local network", note: "recommended"},
		{label: "Everything except the local network and one country's sites",
			note: "domestic sites stay fast and reachable"},
		{label: "Everything"},
	}, def)
	if err != nil {
		return err
	}
	st.Routing = presets[i]
	if st.Routing == xrayconf.RoutingBypassRegion {
		for {
			cc, err := u.ask("Two-letter country code (e.g. ir, cn, ru, tr)", st.Region)
			if err != nil {
				return err
			}
			if len(cc) == 2 {
				st.Region = strings.ToLower(cc)
				break
			}
			u.warn("enter a two-letter code")
		}
	}
	return nil
}

func portBusy(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	l.Close()
	return false
}

// checkConnection waits for Xray to listen, then fetches the probe URL
// through it. In auto mode the balancer needs a moment to measure servers.
func checkConnection(ctx context.Context, u *ui, st manage.State) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", st.SocksPort)
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			u.warn("Xray is not listening on %s; see: journalctl -u %s", addr, layout.UnitName)
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
	tries := 1
	if st.Auto {
		tries = 3
	}
	var r probe.Result
	for i := 0; i < tries; i++ {
		r = probe.Through(ctx, addr, "", 15*time.Second)
		if r.OK() {
			u.ok("connected through the proxy (%d ms)", r.Latency.Milliseconds())
			return true
		}
		if i < tries-1 {
			time.Sleep(5 * time.Second)
		}
	}
	u.warn("no connection through the proxy yet: %v", r.Err)
	u.printf("     %s\n", u.dim("try another server:  sudo sneakernet tui   (press T to test them all)"))
	return false
}

func printSummary(u *ui, st manage.State, command string, connected, live bool) {
	w := u.out
	fmt.Fprintln(w)
	if connected {
		fmt.Fprintln(w, u.bold("Sneakernet is ready."))
	} else {
		fmt.Fprintln(w, u.bold("Sneakernet is installed."))
	}
	fmt.Fprintf(w, "  SOCKS5 proxy   127.0.0.1:%d   (browsers: SOCKS v5, proxy DNS through SOCKS)\n", st.SocksPort)
	fmt.Fprintf(w, "  HTTP proxy     127.0.0.1:%d\n", st.HTTPPort)
	fmt.Fprintf(w, "  Server         %s\n", st.Describe())
	fmt.Fprintf(w, "  Routing        %s\n", routingLabel(st))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Use it in a terminal:")
	writeProxyExports(w, st)
	fmt.Fprintf(w, "  Manage it:      sudo %s tui\n", command)
	fmt.Fprintf(w, "                  %s status | switch | test | uninstall\n", command)
	if live {
		fmt.Fprintln(w, "\n  Live session: this is gone after a reboot unless Ventoy persistence is set up.")
	}
}

func routingLabel(st manage.State) string {
	if st.Routing == xrayconf.RoutingBypassRegion {
		return fmt.Sprintf("%s (%s)", st.Routing, st.Region)
	}
	return st.Routing
}

func writeProxyExports(w io.Writer, st manage.State) {
	h := fmt.Sprintf("http://127.0.0.1:%d", st.HTTPPort)
	fmt.Fprintf(w, "    export http_proxy=%s https_proxy=%s\n", h, h)
	fmt.Fprintf(w, "    export all_proxy=socks5h://127.0.0.1:%d no_proxy=localhost,127.0.0.1,::1\n", st.SocksPort)
}
