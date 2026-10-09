// Package links parses V2Ray/Xray share links (vless://, vmess://, trojan://,
// ss://, hysteria2://) into protocol-neutral Server values.
//
// Parsing never needs the network. A link that parses but cannot be used by
// the pinned Xray core gets a non-empty Problem instead of an error, so the
// UI can still list it and explain why it is skipped.
package links

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Server is one parsed share link.
type Server struct {
	Index    int    // 1-based position among the links of a list (0 when parsed alone)
	Line     int    // 1-based line in the source file (0 when parsed alone)
	Raw      string // the link as given
	Name     string
	Protocol string // vless | vmess | trojan | shadowsocks | hysteria2
	Address  string
	Port     int

	ID         string // vless, vmess
	Password   string // trojan, shadowsocks, hysteria2 auth
	Method     string // shadowsocks cipher; vmess security
	Encryption string // vless: "none" unless VLESS encryption is used
	Flow       string

	Transport Transport
	Security  Security

	// Problem is set when the pinned Xray core cannot use this server.
	Problem string
	// Warnings describe parts of the link that were dropped or ignored.
	Warnings []string
}

// Transport describes the Xray transport ("network") layer.
type Transport struct {
	Network     string // raw | ws | grpc | xhttp | httpupgrade | kcp | hysteria
	HeaderType  string // raw: "http" for HTTP obfuscation; kcp: header type
	Host        string
	Path        string
	ServiceName string // grpc
	Authority   string // grpc
	MultiMode   bool   // grpc mode=multi
	Mode        string // xhttp mode
	Extra       json.RawMessage
	Seed        string // kcp
}

// Security describes the TLS / REALITY layer.
type Security struct {
	Type             string // none | tls | reality
	SNI              string
	Fingerprint      string
	ALPN             []string
	PinnedCertSHA256 string
	VerifyCertByName string
	ECHConfigList    string
	Insecure         bool // the link asked to skip certificate checks
	PublicKey        string
	ShortID          string
	SpiderX          string
	MLDSA65Verify    string
}

// Key identifies a server by its link. Unlike Index it survives adding and
// removing other servers.
func (s *Server) Key() string {
	h := sha256.Sum256([]byte(s.Raw))
	return hex.EncodeToString(h[:6])
}

// Usable reports whether the server can be put into an Xray config.
func (s *Server) Usable() bool { return s.Problem == "" }

// Kind is a short human description, e.g. "vless xhttp/reality".
func (s *Server) Kind() string {
	sec := s.Security.Type
	if sec == "" {
		sec = "none"
	}
	return s.Protocol + " " + s.Transport.Network + "/" + sec
}

func (s *Server) warn(format string, a ...any) {
	s.Warnings = append(s.Warnings, fmt.Sprintf(format, a...))
}

// LineError is a link that could not be parsed at all.
type LineError struct {
	Line int
	Err  error
}

func (e LineError) Error() string { return fmt.Sprintf("line %d: %v", e.Line, e.Err) }

// ParseList reads share links, one per line. Blank lines and lines starting
// with '#' are skipped. A base64-encoded subscription body is decoded first.
func ParseList(r io.Reader) ([]Server, []LineError, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Contains(data, []byte("://")) {
		if dec, err := decodeBase64(string(bytes.Join(bytes.Fields(data), nil))); err == nil &&
			bytes.Contains(dec, []byte("://")) {
			data = dec
		}
	}
	var (
		servers []Server
		errs    []LineError
		line    int
	)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // REALITY ML-DSA keys make long lines
	for sc.Scan() {
		line++
		text := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		s, err := Parse(text)
		if err != nil {
			errs = append(errs, LineError{Line: line, Err: err})
			continue
		}
		s.Line = line
		s.Index = len(servers) + 1
		servers = append(servers, s)
	}
	return servers, errs, sc.Err()
}

// Parse parses a single share link.
func Parse(link string) (Server, error) {
	link = strings.TrimSpace(link)
	i := strings.Index(link, "://")
	if i <= 0 {
		return Server{}, errors.New("not a share link")
	}
	var (
		s   Server
		err error
	)
	switch strings.ToLower(link[:i]) {
	case "vless":
		s, err = parseURLStyle(link, "vless")
	case "trojan":
		s, err = parseURLStyle(link, "trojan")
	case "hysteria2", "hy2":
		s, err = parseURLStyle(link, "hysteria2")
	case "vmess":
		s, err = parseVMess(link)
	case "ss":
		s, err = parseShadowsocks(link)
	default:
		return Server{}, fmt.Errorf("unsupported scheme %q", link[:i])
	}
	if err != nil {
		return Server{}, err
	}
	s.Raw = link
	s.Name = SanitizeName(s.Name)
	if s.Name == "" {
		s.Name = s.Protocol + " " + net.JoinHostPort(s.Address, strconv.Itoa(s.Port))
	}
	s.check()
	return s, nil
}

// parts of a URL-style share link, split leniently: names and passwords in
// the wild contain characters that net/url rejects.
type linkParts struct {
	user, host string
	port       int
	query      map[string]string // keys lower-cased
	name       string
}

func splitLink(link string) (linkParts, error) {
	var p linkParts
	rest := link[strings.Index(link, "://")+3:]
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		p.name = unescape(rest[i+1:])
		rest = rest[:i]
	}
	rawQuery := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rawQuery = rest[i+1:]
		rest = rest[:i]
	}
	at := strings.LastIndexByte(rest, '@')
	if at >= 0 {
		p.user = unescape(rest[:at])
		rest = rest[at+1:]
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	host, port, err := splitHostPort(rest)
	if err != nil {
		return p, err
	}
	p.host, p.port = host, port
	p.query = parseQuery(rawQuery)
	return p, nil
}

func splitHostPort(hostport string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", 0, errors.New("bad host:port (the link is not shown: it may hold credentials)")
	}
	if host == "" {
		return "", 0, errors.New("empty server address")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("bad port %q", portStr)
	}
	return host, port, nil
}

func parseQuery(raw string) map[string]string {
	q := map[string]string{}
	for _, kv := range strings.Split(raw, "&") {
		if kv == "" {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		k = strings.ToLower(unescape(k))
		if _, dup := q[k]; !dup {
			q[k] = unescape(v)
		}
	}
	return q
}

// unescape decodes %XX escapes but keeps '+' (passwords and base64 use it).
func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func parseURLStyle(link, proto string) (Server, error) {
	p, err := splitLink(link)
	if err != nil {
		return Server{}, err
	}
	if p.user == "" {
		return Server{}, errors.New("missing user id / password")
	}
	s := Server{Protocol: proto, Name: p.name, Address: p.host, Port: p.port}
	q := p.query
	switch proto {
	case "vless":
		s.ID = p.user
		s.Encryption = q["encryption"]
		if s.Encryption == "" {
			s.Encryption = "none"
		}
		s.Flow = q["flow"]
	case "trojan":
		s.Password = p.user
		s.Flow = q["flow"]
		if q["security"] == "" {
			q["security"] = "tls" // trojan is TLS unless stated otherwise
		}
	case "hysteria2":
		s.Password = p.user
		q["security"] = "tls"
		q["type"] = "hysteria"
		if q["pinsha256"] != "" && q["pcs"] == "" {
			q["pcs"] = q["pinsha256"]
		}
		if q["obfs"] != "" {
			s.Problem = fmt.Sprintf("hysteria2 obfuscation %q is not supported yet", q["obfs"])
		}
	}
	applyTransport(&s, q)
	applySecurity(&s, q)
	return s, nil
}

func applyTransport(s *Server, q map[string]string) {
	t := &s.Transport
	t.Network = normalizeNetwork(s, q["type"])
	t.HeaderType = q["headertype"]
	t.Host = q["host"]
	t.Path = q["path"]
	t.ServiceName = q["servicename"]
	t.Authority = q["authority"]
	t.Seed = q["seed"]
	switch t.Network {
	case "grpc":
		t.MultiMode = q["mode"] == "multi"
	case "xhttp":
		t.Mode = q["mode"]
		if extra := q["extra"]; extra != "" {
			var obj map[string]any
			if err := json.Unmarshal([]byte(extra), &obj); err != nil {
				s.warn("ignored invalid xhttp \"extra\" JSON")
			} else {
				t.Extra = json.RawMessage(extra)
			}
		}
	}
}

func normalizeNetwork(s *Server, n string) string {
	switch strings.ToLower(n) {
	case "", "tcp", "raw":
		return "raw"
	case "ws", "websocket":
		return "ws"
	case "grpc", "gun":
		return "grpc"
	case "xhttp", "splithttp":
		return "xhttp"
	case "httpupgrade":
		return "httpupgrade"
	case "kcp", "mkcp":
		return "kcp"
	case "hysteria":
		return "hysteria"
	case "h2", "h3", "http", "quic":
		s.Problem = fmt.Sprintf("transport %q was removed from Xray (use xhttp)", n)
		return strings.ToLower(n)
	default:
		s.Problem = fmt.Sprintf("unknown transport %q", n)
		return strings.ToLower(n)
	}
}

func applySecurity(s *Server, q map[string]string) {
	sec := &s.Security
	sec.Type = strings.ToLower(q["security"])
	switch sec.Type {
	case "", "none":
		sec.Type = "none"
	case "tls", "reality":
	case "xtls":
		s.Problem = "legacy XTLS was removed from Xray (use xtls-rprx-vision with TLS or REALITY)"
	default:
		s.Problem = fmt.Sprintf("unknown security %q", sec.Type)
	}
	sec.SNI = q["sni"]
	if sec.SNI == "" {
		sec.SNI = q["peer"]
	}
	sec.Fingerprint = q["fp"]
	if a := q["alpn"]; a != "" {
		for _, v := range strings.Split(a, ",") {
			if v = strings.TrimSpace(v); v != "" {
				sec.ALPN = append(sec.ALPN, v)
			}
		}
	}
	sec.PinnedCertSHA256 = q["pcs"]
	sec.VerifyCertByName = q["vcn"]
	sec.ECHConfigList = q["ech"]
	sec.Insecure = truthy(q["allowinsecure"]) || truthy(q["insecure"])
	sec.PublicKey = q["pbk"]
	sec.ShortID = q["sid"]
	sec.SpiderX = q["spx"]
	sec.MLDSA65Verify = q["pqv"]
}

func truthy(v string) bool { return v == "1" || strings.EqualFold(v, "true") }

func parseVMess(link string) (Server, error) {
	body := link[len("vmess://"):]
	if i := strings.IndexByte(body, '#'); i >= 0 && strings.Contains(body[:i], "@") {
		return parseURLStyleVMess(link)
	}
	dec, err := decodeBase64(body)
	if err != nil {
		if strings.Contains(body, "@") {
			return parseURLStyleVMess(link)
		}
		return Server{}, errors.New("vmess: body is not base64 JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(dec, &m); err != nil {
		return Server{}, fmt.Errorf("vmess: %v", err)
	}
	str := func(k string) string {
		switch v := m[k].(type) {
		case string:
			return strings.TrimSpace(v)
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return ""
	}
	s := Server{Protocol: "vmess", Name: str("ps"), Address: str("add"), ID: str("id"), Method: str("scy")}
	if s.Address == "" || s.ID == "" {
		return Server{}, errors.New("vmess: missing address or id")
	}
	port, err := strconv.Atoi(str("port"))
	if err != nil || port < 1 || port > 65535 {
		return Server{}, fmt.Errorf("vmess: bad port %q", str("port"))
	}
	s.Port = port
	if s.Method == "" {
		s.Method = "auto"
	}
	q := map[string]string{
		"type": str("net"), "host": str("host"), "path": str("path"),
		"security": str("tls"), "sni": str("sni"), "alpn": str("alpn"), "fp": str("fp"),
		"pbk": str("pbk"), "sid": str("sid"), "spx": str("spx"),
	}
	header := str("type") // header type for raw/kcp, mode for grpc/xhttp
	switch normalizeNetwork(&Server{}, q["type"]) {
	case "grpc":
		q["servicename"] = q["path"]
		q["path"] = ""
		q["mode"] = header
	case "xhttp":
		q["mode"] = header
	default:
		if header != "none" {
			q["headertype"] = header
		}
	}
	applyTransport(&s, q)
	applySecurity(&s, q)
	return s, nil
}

func parseURLStyleVMess(link string) (Server, error) {
	p, err := splitLink(link)
	if err != nil {
		return Server{}, err
	}
	s := Server{Protocol: "vmess", Name: p.name, Address: p.host, Port: p.port, ID: p.user,
		Method: p.query["encryption"]}
	if s.Method == "" {
		s.Method = "auto"
	}
	applyTransport(&s, p.query)
	applySecurity(&s, p.query)
	return s, nil
}

// ssCiphers are the Shadowsocks methods Xray implements.
var ssCiphers = map[string]bool{}

func init() {
	for _, c := range []string{
		"aes-128-gcm", "aes-256-gcm",
		"chacha20-poly1305", "chacha20-ietf-poly1305",
		"xchacha20-poly1305", "xchacha20-ietf-poly1305",
		"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
		"none", "plain",
	} {
		ssCiphers[c] = true
	}
}

func parseShadowsocks(link string) (Server, error) {
	body := link[len("ss://"):]
	name := ""
	if i := strings.IndexByte(body, '#'); i >= 0 {
		name = unescape(body[i+1:])
		body = body[:i]
	}
	rawQuery := ""
	if i := strings.IndexByte(body, '?'); i >= 0 {
		rawQuery = body[i+1:]
		body = body[:i]
	}
	body = strings.TrimSuffix(body, "/")

	var userinfo, hostport string
	if at := strings.LastIndexByte(body, '@'); at >= 0 {
		// SIP002: base64(method:password)@host:port, or plain method:password@host:port
		userinfo, hostport = unescape(body[:at]), body[at+1:]
		if dec, err := decodeBase64(userinfo); err == nil && strings.Contains(string(dec), ":") {
			userinfo = string(dec)
		}
	} else {
		// legacy: base64(method:password@host:port)
		dec, err := decodeBase64(unescape(body))
		if err != nil {
			return Server{}, errors.New("ss: body is not base64")
		}
		at := strings.LastIndexByte(string(dec), '@')
		if at < 0 {
			return Server{}, errors.New("ss: missing @host:port")
		}
		userinfo, hostport = string(dec[:at]), string(dec[at+1:])
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok || method == "" {
		return Server{}, errors.New("ss: missing method:password")
	}
	host, port, err := splitHostPort(hostport)
	if err != nil {
		return Server{}, err
	}
	s := Server{Protocol: "shadowsocks", Name: name, Address: host, Port: port,
		Method: strings.ToLower(method), Password: password}
	s.Transport.Network = "raw"
	s.Security.Type = "none"
	if plugin := parseQuery(rawQuery)["plugin"]; plugin != "" {
		s.Problem = fmt.Sprintf("shadowsocks plugin %q is not supported by Xray", strings.SplitN(plugin, ";", 2)[0])
	} else if !ssCiphers[s.Method] {
		s.Problem = fmt.Sprintf("shadowsocks cipher %q is not supported by Xray", s.Method)
	}
	return s, nil
}

// check flags what the pinned Xray core rejects, so the UI can explain it
// instead of surfacing a config-test error.
func (s *Server) check() {
	sec := &s.Security
	if sec.MLDSA65Verify != "" {
		if b, err := base64.RawURLEncoding.DecodeString(sec.MLDSA65Verify); err != nil || len(b) != 1952 {
			s.warn("dropped malformed REALITY post-quantum key (pqv); the standard REALITY check still applies")
			sec.MLDSA65Verify = ""
		}
	}
	if sec.PinnedCertSHA256 != "" {
		for _, v := range strings.Split(sec.PinnedCertSHA256, ",") {
			b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(v), ":", ""))
			if err != nil || len(b) != 32 {
				s.warn("dropped malformed pinned certificate hash (pcs)")
				sec.PinnedCertSHA256 = ""
				break
			}
		}
	}
	if s.Problem != "" {
		return
	}
	switch {
	case sec.Type == "reality":
		if n := s.Transport.Network; n != "raw" && n != "xhttp" && n != "grpc" {
			s.Problem = "REALITY only works with raw, xhttp or grpc transport, not " + n
		} else if b, err := base64.RawURLEncoding.DecodeString(sec.PublicKey); err != nil || len(b) != 32 {
			s.Problem = "REALITY public key (pbk) is missing or invalid"
		} else if len(sec.ShortID) > 16 || !isHex(sec.ShortID) {
			s.Problem = "REALITY short id (sid) is invalid"
		}
	case sec.Type == "none" && (s.Protocol == "vless" || s.Protocol == "trojan") &&
		(s.Encryption == "" || s.Encryption == "none") && isPublicHost(s.Address):
		s.Problem = "no TLS or encryption: Xray refuses plaintext " + s.Protocol + " to a public server"
	}
	if s.Problem == "" && sec.Insecure && sec.PinnedCertSHA256 == "" && sec.Type == "tls" {
		s.warn("link disables certificate checks (allowInsecure), which Xray no longer supports; " +
			"it only connects if the server certificate is valid")
	}
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// isPublicHost approximates Xray's private-address check. The real check runs
// later in `xray run -test`; this only exists to give a readable reason.
func isPublicHost(h string) bool {
	if ip := net.ParseIP(h); ip != nil {
		return !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
	}
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	for _, suf := range []string{"localhost", ".local", ".lan", ".internal", ".home.arpa"} {
		if h == strings.TrimPrefix(suf, ".") || strings.HasSuffix(h, suf) {
			return false
		}
	}
	return true
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("invalid base64")
}

// terminalSeq matches ANSI escape sequences: CSI (ESC [ ... final byte),
// OSC (ESC ] ... BEL or ESC \\), and other two-byte ESC sequences.
var terminalSeq = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?|\x1b[ -~]?`)

// SanitizeName makes a server name from an untrusted link safe to print:
// ANSI/OSC sequences are removed, and control characters (including CR/LF),
// bidi overrides and invalid UTF-8 are dropped. The original link stays in
// Raw. Other text, including emoji and non-Latin scripts, is kept.
func SanitizeName(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = terminalSeq.ReplaceAllString(name, "")
	name = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r):
			return -1
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c:
			return -1 // bidi overrides can reorder what the reader sees
		case r == 0x2028 || r == 0x2029:
			return -1
		}
		return r
	}, name)
	return strings.TrimSpace(name)
}
