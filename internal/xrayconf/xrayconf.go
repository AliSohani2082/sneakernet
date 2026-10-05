// Package xrayconf turns parsed share links into Xray-core JSON configs.
//
// Field names follow the Xray-core version pinned in versions.lock (v26.9.9,
// infra/conf/*.go). Xray ignores unknown JSON keys, so a typo here does not
// fail `xray run -test`; the golden tests in this package are the guard.
package xrayconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/AliSohani2082/sneakernet/internal/links"
)

// Routing presets.
const (
	RoutingGlobal       = "global"        // everything through the proxy
	RoutingBypassLAN    = "bypass-lan"    // private addresses go direct
	RoutingBypassRegion = "bypass-region" // LAN + one country's IPs/TLD go direct
)

// RoutingPresets lists the valid Options.Routing values.
var RoutingPresets = []string{RoutingGlobal, RoutingBypassLAN, RoutingBypassRegion}

// DefaultProbeURL answers 204 with an empty body; used for health checks.
const DefaultProbeURL = "https://www.gstatic.com/generate_204"

// Options control the generated config.
type Options struct {
	Listen    string // inbound listen address; default 127.0.0.1
	SocksPort int    // default 10808
	HTTPPort  int    // default 10809
	Routing   string // one of RoutingPresets; default bypass-lan
	Region    string // ISO 3166 country code for bypass-region, e.g. "ir"
	LogLevel  string // default "warning"
	ProbeURL  string // auto mode health check; default DefaultProbeURL
}

func (o *Options) defaults() error {
	if o.Listen == "" {
		o.Listen = "127.0.0.1"
	}
	if o.SocksPort == 0 {
		o.SocksPort = 10808
	}
	if o.HTTPPort == 0 {
		o.HTTPPort = 10809
	}
	if o.Routing == "" {
		o.Routing = RoutingBypassLAN
	}
	if o.LogLevel == "" {
		o.LogLevel = "warning"
	}
	if o.ProbeURL == "" {
		o.ProbeURL = DefaultProbeURL
	}
	o.Region = strings.ToLower(strings.TrimSpace(o.Region))
	switch o.Routing {
	case RoutingGlobal, RoutingBypassLAN:
	case RoutingBypassRegion:
		if len(o.Region) != 2 {
			return fmt.Errorf("routing %q needs a two-letter country code, got %q", o.Routing, o.Region)
		}
	default:
		return fmt.Errorf("unknown routing preset %q (want one of %s)", o.Routing, strings.Join(RoutingPresets, ", "))
	}
	return nil
}

// Selection picks which server carries traffic.
type Selection struct {
	Auto  bool // leastPing balancer over every usable server
	Index int  // links.Server.Index when !Auto
}

func (s Selection) String() string {
	if s.Auto {
		return "auto"
	}
	return fmt.Sprintf("#%d", s.Index)
}

// Config is the subset of the Xray config schema that sneakernet writes.
type Config struct {
	Log         *Log         `json:"log,omitempty"`
	Inbounds    []Inbound    `json:"inbounds"`
	Outbounds   []Outbound   `json:"outbounds"`
	Routing     *Routing     `json:"routing,omitempty"`
	Observatory *Observatory `json:"observatory,omitempty"`
}

type Log struct {
	LogLevel string `json:"loglevel"`
}

type Inbound struct {
	Tag      string    `json:"tag"`
	Listen   string    `json:"listen"`
	Port     int       `json:"port"`
	Protocol string    `json:"protocol"`
	Settings any       `json:"settings"`
	Sniffing *Sniffing `json:"sniffing,omitempty"`
}

type Sniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
	RouteOnly    bool     `json:"routeOnly"`
}

type Outbound struct {
	Tag            string  `json:"tag"`
	Protocol       string  `json:"protocol"`
	Settings       any     `json:"settings,omitempty"`
	StreamSettings *Stream `json:"streamSettings,omitempty"`
}

type Stream struct {
	Network             string    `json:"network"`
	Security            string    `json:"security,omitempty"`
	TLSSettings         *TLS      `json:"tlsSettings,omitempty"`
	RealitySettings     *Reality  `json:"realitySettings,omitempty"`
	RawSettings         *Raw      `json:"rawSettings,omitempty"`
	WSSettings          *HostPath `json:"wsSettings,omitempty"`
	HTTPUpgradeSettings *HostPath `json:"httpupgradeSettings,omitempty"`
	GRPCSettings        *GRPC     `json:"grpcSettings,omitempty"`
	XHTTPSettings       *XHTTP    `json:"xhttpSettings,omitempty"`
	KCPSettings         *KCP      `json:"kcpSettings,omitempty"`
	HysteriaSettings    *Hysteria `json:"hysteriaSettings,omitempty"`
}

type TLS struct {
	ServerName           string   `json:"serverName,omitempty"`
	Fingerprint          string   `json:"fingerprint,omitempty"`
	ALPN                 []string `json:"alpn,omitempty"`
	PinnedPeerCertSha256 string   `json:"pinnedPeerCertSha256,omitempty"`
	VerifyPeerCertByName string   `json:"verifyPeerCertByName,omitempty"`
	ECHConfigList        string   `json:"echConfigList,omitempty"`
}

type Reality struct {
	ServerName    string `json:"serverName,omitempty"`
	Fingerprint   string `json:"fingerprint"`
	Password      string `json:"password"` // the server's X25519 public key
	ShortID       string `json:"shortId"`
	SpiderX       string `json:"spiderX,omitempty"`
	MLDSA65Verify string `json:"mldsa65Verify,omitempty"`
}

type Raw struct {
	Header any `json:"header"`
}

type HostPath struct {
	Host string `json:"host,omitempty"`
	Path string `json:"path,omitempty"`
}

type GRPC struct {
	ServiceName string `json:"serviceName,omitempty"`
	Authority   string `json:"authority,omitempty"`
	MultiMode   bool   `json:"multiMode,omitempty"`
}

type XHTTP struct {
	Host  string          `json:"host,omitempty"`
	Path  string          `json:"path,omitempty"`
	Mode  string          `json:"mode,omitempty"`
	Extra json.RawMessage `json:"extra,omitempty"`
}

type KCP struct {
	Header any    `json:"header,omitempty"`
	Seed   string `json:"seed,omitempty"`
}

type Hysteria struct {
	Version int    `json:"version"`
	Auth    string `json:"auth"`
}

type Routing struct {
	DomainStrategy string     `json:"domainStrategy"`
	Rules          []Rule     `json:"rules"`
	Balancers      []Balancer `json:"balancers,omitempty"`
}

type Rule struct {
	InboundTag  []string `json:"inboundTag,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Network     string   `json:"network,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	BalancerTag string   `json:"balancerTag,omitempty"`
}

type Balancer struct {
	Tag         string   `json:"tag"`
	Selector    []string `json:"selector"`
	FallbackTag string   `json:"fallbackTag,omitempty"`
	Strategy    Strategy `json:"strategy"`
}

type Strategy struct {
	Type string `json:"type"`
}

type Observatory struct {
	SubjectSelector   []string `json:"subjectSelector"`
	ProbeURL          string   `json:"probeURL"`
	ProbeInterval     string   `json:"probeInterval"`
	EnableConcurrency bool     `json:"enableConcurrency"`
}

// Tags used in generated configs.
const (
	TagSocks    = "socks"
	TagHTTP     = "http"
	TagProxy    = "proxy"  // the single selected server
	TagAuto     = "auto"   // balancer tag in auto mode
	autoPrefix  = "proxy-" // per-server tags in auto mode
	TagDirect   = "direct"
	TagBlock    = "block"
	probePrefix = "probe-"
)

// ErrNoUsable means no server in the list can be used by Xray.
var ErrNoUsable = errors.New("no usable servers")

// Build returns a config that routes traffic through the selected server, or
// through a leastPing balancer over all usable servers when sel.Auto is set.
func Build(servers []links.Server, sel Selection, opts Options) (*Config, error) {
	if err := opts.defaults(); err != nil {
		return nil, err
	}
	sniff := &Sniffing{Enabled: true, DestOverride: []string{"http", "tls", "quic"}, RouteOnly: true}
	c := &Config{
		Log: &Log{LogLevel: opts.LogLevel},
		Inbounds: []Inbound{
			{Tag: TagSocks, Listen: opts.Listen, Port: opts.SocksPort, Protocol: "socks",
				Settings: map[string]any{"auth": "noauth", "udp": true}, Sniffing: sniff},
			{Tag: TagHTTP, Listen: opts.Listen, Port: opts.HTTPPort, Protocol: "http",
				Settings: map[string]any{}, Sniffing: sniff},
		},
		Routing: &Routing{DomainStrategy: "AsIs", Rules: directRules(opts)},
	}

	if sel.Auto {
		var tags []string
		for i := range servers {
			s := &servers[i]
			if !s.Usable() {
				continue
			}
			tag := fmt.Sprintf("%s%03d", autoPrefix, s.Index)
			c.Outbounds = append(c.Outbounds, ServerOutbound(s, tag))
			tags = append(tags, tag)
		}
		if len(tags) == 0 {
			return nil, ErrNoUsable
		}
		c.Routing.Balancers = []Balancer{{
			Tag: TagAuto, Selector: []string{autoPrefix}, FallbackTag: tags[0],
			Strategy: Strategy{Type: "leastPing"},
		}}
		c.Routing.Rules = append(c.Routing.Rules, Rule{Network: "tcp,udp", BalancerTag: TagAuto})
		c.Observatory = &Observatory{
			SubjectSelector: []string{autoPrefix}, ProbeURL: opts.ProbeURL,
			ProbeInterval: "1m", EnableConcurrency: true,
		}
	} else {
		s := find(servers, sel.Index)
		if s == nil {
			return nil, fmt.Errorf("no server #%d", sel.Index)
		}
		if !s.Usable() {
			return nil, fmt.Errorf("server #%d cannot be used: %s", s.Index, s.Problem)
		}
		// The first outbound is Xray's default route.
		c.Outbounds = append(c.Outbounds, ServerOutbound(s, TagProxy))
	}
	c.Outbounds = append(c.Outbounds,
		Outbound{Tag: TagDirect, Protocol: "freedom"},
		Outbound{Tag: TagBlock, Protocol: "blackhole"},
	)
	return c, nil
}

// BuildProbe returns a config with one SOCKS inbound per usable server, each
// routed to its own server, so a single Xray process can test every server
// at once. ports must have an entry per usable server; the returned map gives
// the port used for each server index.
func BuildProbe(servers []links.Server, listen string, ports []int) (*Config, map[int]int, error) {
	if listen == "" {
		listen = "127.0.0.1"
	}
	c := &Config{Log: &Log{LogLevel: "none"}, Routing: &Routing{DomainStrategy: "AsIs"}}
	portOf := map[int]int{}
	for i := range servers {
		s := &servers[i]
		if !s.Usable() {
			continue
		}
		if len(portOf) >= len(ports) {
			return nil, nil, errors.New("not enough ports for the probe config")
		}
		port := ports[len(portOf)]
		in := fmt.Sprintf("%sin-%d", probePrefix, s.Index)
		out := fmt.Sprintf("%sout-%d", probePrefix, s.Index)
		c.Inbounds = append(c.Inbounds, Inbound{Tag: in, Listen: listen, Port: port, Protocol: "socks",
			Settings: map[string]any{"auth": "noauth", "udp": false}})
		c.Outbounds = append(c.Outbounds, ServerOutbound(s, out))
		c.Routing.Rules = append(c.Routing.Rules, Rule{InboundTag: []string{in}, OutboundTag: out})
		portOf[s.Index] = port
	}
	if len(portOf) == 0 {
		return nil, nil, ErrNoUsable
	}
	return c, portOf, nil
}

// JSON renders the config.
func (c *Config) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func find(servers []links.Server, index int) *links.Server {
	for i := range servers {
		if servers[i].Index == index {
			return &servers[i]
		}
	}
	return nil
}

func directRules(o Options) []Rule {
	if o.Routing == RoutingGlobal {
		return nil
	}
	rules := []Rule{
		{IP: []string{"geoip:private"}, OutboundTag: TagDirect},
		{Domain: []string{"geosite:private"}, OutboundTag: TagDirect},
	}
	if o.Routing == RoutingBypassRegion {
		rules = append(rules,
			Rule{Domain: []string{"domain:" + o.Region}, OutboundTag: TagDirect},
			Rule{IP: []string{"geoip:" + o.Region}, OutboundTag: TagDirect},
		)
	}
	return rules
}

// ServerOutbound converts one server into an Xray outbound with the given tag.
func ServerOutbound(s *links.Server, tag string) Outbound {
	o := Outbound{Tag: tag, Protocol: s.Protocol}
	switch s.Protocol {
	case "vless":
		set := map[string]any{"address": s.Address, "port": s.Port, "id": s.ID, "encryption": s.Encryption}
		if s.Flow != "" {
			set["flow"] = s.Flow
		}
		o.Settings = set
	case "vmess":
		o.Settings = map[string]any{"address": s.Address, "port": s.Port, "id": s.ID, "security": s.Method}
	case "trojan":
		set := map[string]any{"address": s.Address, "port": s.Port, "password": s.Password}
		if s.Flow != "" {
			set["flow"] = s.Flow
		}
		o.Settings = set
	case "shadowsocks":
		o.Settings = map[string]any{"address": s.Address, "port": s.Port, "method": s.Method, "password": s.Password}
	case "hysteria2":
		o.Protocol = "hysteria"
		o.Settings = map[string]any{"version": 2, "address": s.Address, "port": s.Port}
	}
	o.StreamSettings = stream(s)
	return o
}

func stream(s *links.Server) *Stream {
	t, sec := &s.Transport, &s.Security
	st := &Stream{Network: t.Network}
	switch t.Network {
	case "raw":
		if t.HeaderType == "http" {
			path := t.Path
			if path == "" {
				path = "/"
			}
			req := map[string]any{"path": strings.Split(path, ",")}
			if t.Host != "" {
				req["headers"] = map[string]any{"Host": strings.Split(t.Host, ",")}
			}
			st.RawSettings = &Raw{Header: map[string]any{"type": "http", "request": req}}
		}
	case "ws":
		st.WSSettings = &HostPath{Host: t.Host, Path: t.Path}
	case "httpupgrade":
		st.HTTPUpgradeSettings = &HostPath{Host: t.Host, Path: t.Path}
	case "grpc":
		st.GRPCSettings = &GRPC{ServiceName: t.ServiceName, Authority: t.Authority, MultiMode: t.MultiMode}
	case "xhttp":
		st.XHTTPSettings = &XHTTP{Host: t.Host, Path: t.Path, Mode: t.Mode, Extra: t.Extra}
	case "kcp":
		k := &KCP{Seed: t.Seed}
		if t.HeaderType != "" && t.HeaderType != "none" {
			k.Header = map[string]any{"type": t.HeaderType}
		}
		st.KCPSettings = k
	case "hysteria":
		st.HysteriaSettings = &Hysteria{Version: 2, Auth: s.Password}
	}
	switch sec.Type {
	case "tls":
		st.Security = "tls"
		st.TLSSettings = &TLS{
			ServerName: sec.SNI, Fingerprint: sec.Fingerprint, ALPN: sec.ALPN,
			PinnedPeerCertSha256: sec.PinnedCertSHA256, VerifyPeerCertByName: sec.VerifyCertByName,
			ECHConfigList: sec.ECHConfigList,
		}
	case "reality":
		fp := sec.Fingerprint
		if fp == "" {
			fp = "chrome" // Xray rejects REALITY without a fingerprint
		}
		st.Security = "reality"
		st.RealitySettings = &Reality{
			ServerName: sec.SNI, Fingerprint: fp, Password: sec.PublicKey, ShortID: sec.ShortID,
			SpiderX: sec.SpiderX, MLDSA65Verify: sec.MLDSA65Verify,
		}
	}
	return st
}
