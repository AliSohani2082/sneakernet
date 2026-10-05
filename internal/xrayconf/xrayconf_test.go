package xrayconf

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AliSohani2082/sneakernet/internal/links"
)

const fixture = "../../test/fixtures/servers.txt"

func loadFixture(t *testing.T) []links.Server {
	t.Helper()
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	servers, errs, err := links.ParseList(f)
	if err != nil || len(errs) != 0 {
		t.Fatalf("fixture: err=%v line errors=%v", err, errs)
	}
	return servers
}

// xrayBinary finds an Xray build to validate against: $SNEAKERNET_XRAY, or the
// copy `make dev-xray` unpacks into .cache. Tests that need it skip otherwise.
func xrayBinary(t *testing.T) (bin, assets string) {
	t.Helper()
	bin = os.Getenv("SNEAKERNET_XRAY")
	if bin == "" {
		bin, _ = filepath.Abs("../../.cache/xray-amd64/xray")
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skip("no xray binary; run `make dev-xray` or set SNEAKERNET_XRAY")
	}
	return bin, filepath.Dir(bin)
}

func render(t *testing.T, c *Config) []byte {
	t.Helper()
	b, err := c.JSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEveryUsableServerPassesXrayTest(t *testing.T) {
	bin, assets := xrayBinary(t)
	for _, s := range loadFixture(t) {
		if !s.Usable() {
			continue
		}
		t.Run(s.Name, func(t *testing.T) {
			t.Parallel()
			c, err := Build([]links.Server{s}, Selection{Index: s.Index}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(context.Background(), bin, assets, render(t, c)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAutoAndRegionPassXrayTest(t *testing.T) {
	bin, assets := xrayBinary(t)
	servers := loadFixture(t)
	for _, opts := range []Options{
		{Routing: RoutingGlobal},
		{Routing: RoutingBypassLAN},
		{Routing: RoutingBypassRegion, Region: "IR"},
	} {
		c, err := Build(servers, Selection{Auto: true}, opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(context.Background(), bin, assets, render(t, c)); err != nil {
			t.Fatalf("%s: %v", opts.Routing, err)
		}
	}
	ports := make([]int, len(servers))
	for i := range ports {
		ports[i] = 20000 + i
	}
	c, _, err := BuildProbe(servers, "", ports)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(context.Background(), bin, assets, render(t, c)); err != nil {
		t.Fatalf("probe config: %v", err)
	}
}

func TestValidateReportsXrayError(t *testing.T) {
	bin, assets := xrayBinary(t)
	bad := []byte(`{"outbounds":[{"protocol":"vless","settings":{"address":"8.8.8.8","port":80,"id":"x","encryption":"none"}}]}`)
	err := Validate(context.Background(), bin, assets, bad)
	if err == nil || !strings.Contains(err.Error(), "xray rejected the config") {
		t.Fatalf("want xray error, got %v", err)
	}
}

// TestShape pins the JSON field names; Xray silently ignores unknown keys, so
// `xray run -test` alone would not catch a misspelled one.
func TestShape(t *testing.T) {
	servers := loadFixture(t)
	get := func(name string) map[string]any {
		t.Helper()
		for _, s := range servers {
			if s.Name == name {
				c, err := Build(servers, Selection{Index: s.Index}, Options{Routing: RoutingBypassRegion, Region: "ir"})
				if err != nil {
					t.Fatal(err)
				}
				var m map[string]any
				if err := json.Unmarshal(render(t, c), &m); err != nil {
					t.Fatal(err)
				}
				return m
			}
		}
		t.Fatalf("no fixture server %q", name)
		return nil
	}
	path := func(m any, keys ...string) any {
		for _, k := range keys {
			switch v := m.(type) {
			case map[string]any:
				m = v[k]
			case []any:
				m = v[0]
				if k != "0" {
					m = m.(map[string]any)[k]
				}
			}
		}
		return m
	}

	m := get("vless raw reality")
	ob := path(m, "outbounds", "0")
	if path(ob, "tag") != TagProxy || path(ob, "protocol") != "vless" ||
		path(ob, "settings", "flow") != "xtls-rprx-vision" || path(ob, "settings", "encryption") != "none" {
		t.Errorf("vless outbound: %v", ob)
	}
	rs := path(ob, "streamSettings", "realitySettings").(map[string]any)
	if rs["password"] == "" || rs["fingerprint"] != "chrome" || rs["shortId"] != "6ba85179e30d4fc2" ||
		path(ob, "streamSettings", "network") != "raw" {
		t.Errorf("reality settings: %v", rs)
	}
	rules := path(m, "routing", "rules").([]any)
	if len(rules) != 4 || path(rules[3], "ip").([]any)[0] != "geoip:ir" {
		t.Errorf("bypass-region rules: %v", rules)
	}

	xh := path(get("vless xhttp reality pq"), "outbounds", "0", "streamSettings")
	if path(xh, "xhttpSettings", "extra", "xPaddingBytes") != "100-1000" ||
		len(path(xh, "realitySettings", "mldsa65Verify").(string)) != 2603 {
		t.Errorf("xhttp stream: %v", xh)
	}

	hy := path(get("hysteria2"), "outbounds", "0")
	if path(hy, "protocol") != "hysteria" || path(hy, "settings", "version") != 2.0 ||
		path(hy, "streamSettings", "network") != "hysteria" ||
		path(hy, "streamSettings", "hysteriaSettings", "auth") != "dummy-auth" {
		t.Errorf("hysteria2 outbound: %v", hy)
	}

	grpc := path(get("vless grpc tls"), "outbounds", "0", "streamSettings", "grpcSettings")
	if path(grpc, "serviceName") != "gsvc" || path(grpc, "multiMode") != true {
		t.Errorf("grpc: %v", grpc)
	}
}

func TestAutoMode(t *testing.T) {
	servers := loadFixture(t)
	c, err := Build(servers, Selection{Auto: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	usable := 0
	for _, s := range servers {
		if s.Usable() {
			usable++
		}
	}
	// usable servers + direct + block
	if len(c.Outbounds) != usable+2 || c.Observatory == nil || len(c.Routing.Balancers) != 1 {
		t.Fatalf("auto: %d outbounds, observatory=%v balancers=%v", len(c.Outbounds), c.Observatory, c.Routing.Balancers)
	}
	b := c.Routing.Balancers[0]
	if b.FallbackTag != c.Outbounds[0].Tag || b.Strategy.Type != "leastPing" {
		t.Errorf("balancer: %+v", b)
	}
	last := c.Routing.Rules[len(c.Routing.Rules)-1]
	if last.BalancerTag != TagAuto {
		t.Errorf("last rule should send everything to the balancer: %+v", last)
	}
}

func TestBuildErrors(t *testing.T) {
	servers := loadFixture(t)
	unusable := servers[len(servers)-1]
	if unusable.Usable() {
		t.Fatal("fixture: last server should be unusable")
	}
	if _, err := Build(servers, Selection{Index: unusable.Index}, Options{}); err == nil ||
		!strings.Contains(err.Error(), "cannot be used") {
		t.Errorf("unusable selection: %v", err)
	}
	if _, err := Build(servers, Selection{Index: 999}, Options{}); err == nil {
		t.Error("missing index: want error")
	}
	if _, err := Build([]links.Server{unusable}, Selection{Auto: true}, Options{}); err != ErrNoUsable {
		t.Errorf("no usable: %v", err)
	}
	if _, err := Build(servers, Selection{Auto: true}, Options{Routing: RoutingBypassRegion}); err == nil {
		t.Error("bypass-region without region: want error")
	}
	if _, err := Build(servers, Selection{Auto: true}, Options{Routing: "nope"}); err == nil {
		t.Error("unknown routing: want error")
	}
}
