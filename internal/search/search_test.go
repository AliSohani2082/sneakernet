package search

import (
	"reflect"
	"strings"
	"testing"

	"github.com/AliSohani2082/sneakernet/internal/links"
)

const uuid = "a8d31bbb-0d00-4762-b870-8c23e19d0a8c"
const pbk = "3D8x05l98bV_wuYKxnyW_SAodVkQkOz6LWhCkiT07DY"

func servers(t *testing.T) []links.Server {
	t.Helper()
	list := strings.Join([]string{
		"vless://" + uuid + "@de1.example.com:443?type=xhttp&security=tls&sni=cdn.example.org#🇩🇪 Berlin fast",
		"vless://" + uuid + "@nl.example.net:443?type=tcp&security=reality&pbk=" + pbk + "&sid=ab&sni=www.apple.com#Amsterdam 2",
		"trojan://pw@tr.example.com:8443?sni=tr.example.com#Frankfurt reality-ready",
		"vless://" + uuid + "@1.2.3.4:80?type=ws#plaintext Berlin",
		"ss://" + "Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwdw" + "@ss.example.org:8388#Germany backup",
	}, "\n")
	s, errs, err := links.ParseList(strings.NewReader(list))
	if err != nil || len(errs) != 0 {
		t.Fatalf("fixture: %v %v", err, errs)
	}
	return s
}

func names(all []links.Server, hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, all[h.Pos].Name)
	}
	return out
}

func TestEmptyQueryKeepsOrderUsableFirst(t *testing.T) {
	all := servers(t)
	got := names(all, Search(all, "  "))
	want := []string{"🇩🇪 Berlin fast", "Amsterdam 2", "Frankfurt reality-ready", "Germany backup", "plaintext Berlin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestNameBeforeProperties(t *testing.T) {
	all := servers(t)
	hits := Search(all, "reality")
	got := names(all, hits)
	// "Frankfurt reality-ready" matches in its name; "Amsterdam 2" only by security.
	if !reflect.DeepEqual(got, []string{"Frankfurt reality-ready", "Amsterdam 2"}) {
		t.Fatalf("got %q", got)
	}
	if hits[0].Field != "" || hits[1].Field != "sec" {
		t.Errorf("fields: %q %q", hits[0].Field, hits[1].Field)
	}
}

func TestWordStartAndHighlight(t *testing.T) {
	all := servers(t)
	hits := Search(all, "ber")
	got := names(all, hits)
	if len(got) != 2 || got[0] != "🇩🇪 Berlin fast" || got[1] != "plaintext Berlin" {
		t.Fatalf("got %q", got)
	}
	// The flag is two runes plus a space, so "Ber" starts at rune 3.
	if !reflect.DeepEqual(hits[0].NameRunes, []int{3, 4, 5}) {
		t.Errorf("highlight %v", hits[0].NameRunes)
	}
}

func TestFuzzyName(t *testing.T) {
	all := servers(t)
	hits := Search(all, "grmny")
	if got := names(all, hits); len(got) != 1 || got[0] != "Germany backup" {
		t.Fatalf("got %q", got)
	}
	if len(hits[0].NameRunes) != 5 {
		t.Errorf("fuzzy highlight %v", hits[0].NameRunes)
	}
}

func TestPropertyAndQualifiers(t *testing.T) {
	all := servers(t)
	for q, want := range map[string][]string{
		"apple.com":           {"Amsterdam 2"},                        // sni
		"sec:reality":         {"Amsterdam 2"},                        // qualifier
		"port:8443":           {"Frankfurt reality-ready"},            // qualifier
		"proto:vless berlin":  {"🇩🇪 Berlin fast", "plaintext Berlin"}, // qualifier + term
		"net:xhttp":           {"🇩🇪 Berlin fast"},                     // alias of transport
		"transport:ws":        {"plaintext Berlin"},                   // alias
		"berlin fast":         {"🇩🇪 Berlin fast"},                     // all terms must match
		"foo:bar":             nil,                                    // unknown key is a plain term
		"shadowsocks":         {"Germany backup"},                     // protocol
		"host:example.org":    {"Germany backup"},                     // host only, not sni
		"sni:example.org":     {"🇩🇪 Berlin fast"},                     // sni only
		"example.net reality": {"Amsterdam 2"},                        // host + security
	} {
		if got := names(all, Search(all, q)); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %q, want %q", q, got, want)
		}
	}
}

func TestScatteredLettersDoNotMatch(t *testing.T) {
	all := servers(t)
	// p, b, n appear in order in "plaintext Berlin" but spread over 16 runes.
	if got := names(all, Search(all, "pbn")); got != nil {
		t.Errorf("scattered fuzzy match: %q", got)
	}
}

func TestParse(t *testing.T) {
	q := Parse("DE  sec:Reality foo:bar")
	if !reflect.DeepEqual(q.Terms, []string{"de", "foo:bar"}) || len(q.Filters) != 1 ||
		q.Filters[0].Field.Name != "sec" || q.Filters[0].Value != "reality" {
		t.Errorf("parse: %+v", q)
	}
	if !Parse("   ").Empty() {
		t.Error("blank query should be empty")
	}
}
