// Package probe checks whether traffic actually gets through a server.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AliSohani2082/sneakernet/internal/links"
	"github.com/AliSohani2082/sneakernet/internal/xrayconf"
)

// Result of one probe.
type Result struct {
	Latency time.Duration
	Err     error
}

func (r Result) OK() bool { return r.Err == nil }

func (r Result) String() string {
	if r.Err != nil {
		return "failed: " + r.Err.Error()
	}
	return fmt.Sprintf("%d ms", r.Latency.Milliseconds())
}

// Through fetches probeURL through a local SOCKS5 proxy.
func Through(ctx context.Context, socksAddr, probeURL string, timeout time.Duration) Result {
	if probeURL == "" {
		probeURL = xrayconf.DefaultProbeURL
	}
	proxy := &url.URL{Scheme: "socks5h", Host: socksAddr}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:             http.ProxyURL(proxy),
			DisableKeepAlives: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return Result{Err: err}
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{Err: simplify(err)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return Result{Err: fmt.Errorf("unexpected HTTP %d", resp.StatusCode)}
	}
	return Result{Latency: time.Since(start)}
}

func simplify(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "Client.Timeout") {
		return errors.New("timed out")
	}
	return err
}

// All tests every usable server at once with one temporary Xray process that
// has a separate SOCKS port per server. Unusable servers are left out.
func All(ctx context.Context, xrayBin, assetDir string, servers []links.Server, probeURL string,
	timeout time.Duration) (map[int]Result, error) {
	var usable int
	for _, s := range servers {
		if s.Usable() {
			usable++
		}
	}
	if usable == 0 {
		return nil, xrayconf.ErrNoUsable
	}
	ports, err := freePorts(usable)
	if err != nil {
		return nil, err
	}
	cfg, portOf, err := xrayconf.BuildProbe(servers, "127.0.0.1", ports)
	if err != nil {
		return nil, err
	}
	b, err := cfg.JSON()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "sneakernet-probe-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := exec.CommandContext(runCtx, xrayBin, "run", "-c", path)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assetDir)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() { stop(); <-exited }()

	if err := waitListening(ctx, ports[0], exited); err != nil {
		return nil, fmt.Errorf("probe xray did not start: %v: %s", err, lastLine(out.String()))
	}

	results := make(map[int]Result, len(portOf))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for idx, port := range portOf {
		wg.Add(1)
		go func(idx, port int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r := Through(ctx, fmt.Sprintf("127.0.0.1:%d", port), probeURL, timeout)
			mu.Lock()
			results[idx] = r
			mu.Unlock()
		}(idx, port)
	}
	wg.Wait()
	return results, nil
}

func freePorts(n int) ([]int, error) {
	ls := make([]net.Listener, 0, n)
	defer func() {
		for _, l := range ls {
			l.Close()
		}
	}()
	ports := make([]int, 0, n)
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		ls = append(ls, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

func waitListening(ctx context.Context, port int, exited <-chan struct{}) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return errors.New("xray exited")
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
			c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timed out")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
