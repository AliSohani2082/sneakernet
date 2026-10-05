package links

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

const (
	uuid = "a8d31bbb-0d00-4762-b870-8c23e19d0a8c"
	pbk  = "3D8x05l98bV_wuYKxnyW_SAodVkQkOz6LWhCkiT07DY"
	enc  = "mlkem768x25519plus.native.0rtt.YB0Vb6wFFbgciBR1UYXzZnIQVkI3gmfKQnnjeLasVEM"
)

func mustParse(t *testing.T, link string) Server {
	t.Helper()
	s, err := Parse(link)
	if err != nil {
		t.Fatalf("Parse(%q): %v", link, err)
	}
	return s
}

func TestVLESSReality(t *testing.T) {
	s := mustParse(t, "vless://"+uuid+"@example.com:443?type=tcp&security=reality&sni=www.microsoft.com"+
		"&fp=firefox&pbk="+pbk+"&sid=6ba85179e30d4fc2&spx=%2F&flow=xtls-rprx-vision#DE%20%F0%9F%87%A9%F0%9F%87%AA%20reality")
	if s.Protocol != "vless" || s.Address != "example.com" || s.Port != 443 || s.ID != uuid {
		t.Fatalf("basic fields: %+v", s)
	}
	if s.Name != "DE 🇩🇪 reality" {
		t.Errorf("name = %q", s.Name)
	}
	if s.Transport.Network != "raw" || s.Security.Type != "reality" || s.Flow != "xtls-rprx-vision" {
		t.Errorf("kind = %s flow=%s", s.Kind(), s.Flow)
	}
	if s.Security.PublicKey != pbk || s.Security.ShortID != "6ba85179e30d4fc2" || s.Security.SpiderX != "/" ||
		s.Security.SNI != "www.microsoft.com" || s.Security.Fingerprint != "firefox" {
		t.Errorf("reality fields: %+v", s.Security)
	}
	if !s.Usable() || s.Encryption != "none" {
		t.Errorf("usable=%v problem=%q encryption=%q", s.Usable(), s.Problem, s.Encryption)
	}
}

func TestVLESSXHTTPExtraAndPQV(t *testing.T) {
	pqv, err := os.ReadFile("testdata/pqv.txt")
	if err != nil {
		t.Fatal(err)
	}
	extra := `{"xPaddingBytes":"100-1000","xmux":{"maxConcurrency":"16-32"}}`
	link := "vless://" + uuid + "@[2001:db8::1]:8443/?type=xhttp&mode=stream-one&path=%2Fapi&host=cdn.example.org" +
		"&security=reality&pbk=" + pbk + "&sid=&pqv=" + strings.TrimSpace(string(pqv)) +
		"&extra=" + strings.ReplaceAll(extra, `"`, "%22") + "#xhttp"
	s := mustParse(t, link)
	if s.Address != "2001:db8::1" || s.Port != 8443 {
		t.Errorf("ipv6 host: %q %d", s.Address, s.Port)
	}
	if s.Transport.Network != "xhttp" || s.Transport.Mode != "stream-one" || s.Transport.Path != "/api" ||
		s.Transport.Host != "cdn.example.org" || string(s.Transport.Extra) != extra {
		t.Errorf("xhttp: %+v", s.Transport)
	}
	if s.Security.MLDSA65Verify == "" || len(s.Warnings) != 0 {
		t.Errorf("valid pqv dropped: warnings=%v", s.Warnings)
	}

	// A truncated pqv is dropped with a warning; the server stays usable.
	s = mustParse(t, strings.Replace(link, "&pqv=", "&pqv=AAAA", 1)[:len(link)-200]+"#cut")
	if s.Security.MLDSA65Verify != "" || len(s.Warnings) == 0 || !s.Usable() {
		t.Errorf("truncated pqv: pqv=%d warnings=%v problem=%q", len(s.Security.MLDSA65Verify), s.Warnings, s.Problem)
	}
}

func TestProblems(t *testing.T) {
	for _, tc := range []struct{ link, want string }{
		{"vless://" + uuid + "@1.2.3.4:80?type=ws&path=%2F#plain", "plaintext vless"},
		{"vless://" + uuid + "@example.com:443?type=ws&security=reality&pbk=" + pbk, "REALITY only works"},
		{"vless://" + uuid + "@example.com:443?security=reality&pbk=short", "public key"},
		{"vless://" + uuid + "@example.com:443?type=h2&security=tls", "removed from Xray"},
		{"trojan://pw@example.com:443?security=none", "plaintext trojan"},
		{"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-cfb:pw")) + "@example.com:8388", "cipher"},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pw")) + "@example.com:8388/?plugin=obfs-local%3Bobfs%3Dhttp", "plugin"},
		{"hysteria2://pw@example.com:443?obfs=salamander&obfs-password=x", "obfuscation"},
	} {
		s := mustParse(t, tc.link)
		if !strings.Contains(s.Problem, tc.want) {
			t.Errorf("%s\n  problem = %q, want it to mention %q", tc.link, s.Problem, tc.want)
		}
	}
	// Plaintext VLESS is fine with VLESS encryption or to a private address.
	for _, link := range []string{
		"vless://" + uuid + "@example.com:80?type=raw&encryption=" + enc,
		"vless://" + uuid + "@192.168.1.10:80?type=raw",
	} {
		if s := mustParse(t, link); !s.Usable() {
			t.Errorf("%s: unexpected problem %q", link, s.Problem)
		}
	}
}

func TestInsecureWarning(t *testing.T) {
	s := mustParse(t, "trojan://p%40ss@example.com:443?sni=example.com&allowInsecure=1#t")
	if s.Password != "p@ss" || s.Security.Type != "tls" {
		t.Errorf("trojan: %+v", s)
	}
	if !s.Usable() || len(s.Warnings) != 1 {
		t.Errorf("want usable with one warning, got problem=%q warnings=%v", s.Problem, s.Warnings)
	}
}

func TestVMess(t *testing.T) {
	js := `{"v":"2","ps":"vm ws","add":"vm.example.com","port":"443","id":"` + uuid +
		`","aid":0,"scy":"","net":"ws","type":"none","host":"h.example.com","path":"/ws","tls":"tls","sni":"h.example.com","alpn":"h2,http/1.1"}`
	s := mustParse(t, "vmess://"+base64.StdEncoding.EncodeToString([]byte(js)))
	if s.Name != "vm ws" || s.Port != 443 || s.Method != "auto" || s.Transport.Network != "ws" ||
		s.Transport.Path != "/ws" || s.Transport.HeaderType != "" || s.Security.Type != "tls" ||
		len(s.Security.ALPN) != 2 {
		t.Errorf("vmess ws: %+v", s)
	}
	js = `{"ps":"vm grpc","add":"1.1.1.1","port":443,"id":"` + uuid + `","net":"grpc","type":"multi","path":"svc","tls":"tls"}`
	s = mustParse(t, "vmess://"+base64.RawStdEncoding.EncodeToString([]byte(js)))
	if s.Transport.ServiceName != "svc" || !s.Transport.MultiMode || s.Transport.Path != "" {
		t.Errorf("vmess grpc: %+v", s.Transport)
	}
}

func TestShadowsocksForms(t *testing.T) {
	for _, link := range []string{
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pa:ss")) + "@ss.example.com:8388#sip002",
		"ss://2022-blake3-aes-128-gcm:pa%3Ass@ss.example.com:8388#plain",
		"ss://" + base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pa:ss@ss.example.com:8388")) + "#legacy",
	} {
		s := mustParse(t, link)
		if s.Address != "ss.example.com" || s.Port != 8388 || s.Password != "pa:ss" || !s.Usable() {
			t.Errorf("%s: %+v", link, s)
		}
	}
}

func TestHysteria2(t *testing.T) {
	s := mustParse(t, "hy2://user:secret@hy.example.com:8443/?sni=hy.example.com&insecure=1"+
		"&pinSHA256=AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89#hy")
	if s.Protocol != "hysteria2" || s.Password != "user:secret" || s.Transport.Network != "hysteria" ||
		s.Security.Type != "tls" || s.Security.PinnedCertSHA256 == "" || len(s.Warnings) != 0 || !s.Usable() {
		t.Errorf("hysteria2: %+v", s)
	}
}

func TestParseListAndSubscription(t *testing.T) {
	list := strings.Join([]string{
		"# my servers",
		"",
		"vless://" + uuid + "@a.example.com:443?security=tls&type=ws#A",
		"not a link",
		"socks://user@host:1080#unsupported",
		"trojan://pw@b.example.com:443#B",
	}, "\n")
	servers, errs, err := ParseList(strings.NewReader(list))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].Index != 1 || servers[1].Index != 2 || servers[1].Line != 6 {
		t.Fatalf("servers: %+v", servers)
	}
	if len(errs) != 2 || errs[0].Line != 4 || errs[1].Line != 5 {
		t.Fatalf("errs: %v", errs)
	}

	sub := base64.StdEncoding.EncodeToString([]byte(list))
	servers, _, err = ParseList(strings.NewReader(sub[:40] + "\n" + sub[40:]))
	if err != nil || len(servers) != 2 {
		t.Fatalf("subscription: %d servers, err=%v", len(servers), err)
	}
}
