// Package search ranks servers for the TUI's search box.
//
// Matches in the server name always rank above matches in other properties
// (protocol, transport, security, host, SNI, …). Within the name, a whole
// substring beats a fuzzy match, and a match at the start of a word beats
// one in the middle. A query is whitespace-separated terms that must all
// match; a term like "sec:reality" or "port:443" only checks that field.
package search

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/sahilm/fuzzy"

	"github.com/AliSohani2082/sneakernet/internal/links"
)

// Field is a searchable server property. Its name and aliases can be used
// as qualifiers in a query ("sec:reality").
type Field struct {
	Name    string
	Aliases []string
	Value   func(*links.Server) string
}

// Fields are searched in this order after the name.
var Fields = []Field{
	{"proto", []string{"protocol"}, func(s *links.Server) string { return s.Protocol }},
	{"net", []string{"transport", "network"}, func(s *links.Server) string { return s.Transport.Network }},
	{"sec", []string{"security", "tls"}, func(s *links.Server) string { return s.Security.Type }},
	{"host", []string{"address", "server", "addr"}, func(s *links.Server) string { return s.Address }},
	{"port", nil, func(s *links.Server) string { return strconv.Itoa(s.Port) }},
	{"sni", []string{"servername"}, func(s *links.Server) string { return s.Security.SNI }},
	{"path", []string{"service"}, func(s *links.Server) string {
		return strings.TrimSpace(s.Transport.Path + " " + s.Transport.ServiceName + " " + s.Transport.Host)
	}},
	{"flow", nil, func(s *links.Server) string { return s.Flow }},
	{"fp", []string{"fingerprint"}, func(s *links.Server) string { return s.Security.Fingerprint }},
}

func fieldByName(name string) *Field {
	name = strings.ToLower(name)
	for i := range Fields {
		if Fields[i].Name == name {
			return &Fields[i]
		}
		for _, a := range Fields[i].Aliases {
			if a == name {
				return &Fields[i]
			}
		}
	}
	return nil
}

// Query is a parsed search string.
type Query struct {
	Terms   []string // free-text terms, lower-case
	Filters []Filter // field qualifiers
}

// Filter restricts results to servers whose field contains Value.
type Filter struct {
	Field *Field
	Value string // lower-case
}

// Empty reports whether the query matches everything.
func (q Query) Empty() bool { return len(q.Terms) == 0 && len(q.Filters) == 0 }

// Parse splits a search string into terms and field qualifiers. "key:value"
// with an unknown key is treated as a plain term (names can contain colons).
func Parse(s string) Query {
	var q Query
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if k, v, ok := strings.Cut(w, ":"); ok && v != "" {
			if f := fieldByName(k); f != nil {
				q.Filters = append(q.Filters, Filter{Field: f, Value: v})
				continue
			}
		}
		q.Terms = append(q.Terms, w)
	}
	return q
}

// Hit is one search result.
type Hit struct {
	Pos       int    // index into the searched slice
	Score     int    // higher is better
	NameRunes []int  // rune positions in the name that matched, for highlighting
	Field     string // property that matched when a term was not in the name
}

// Score bands. Any name match outranks any property-only match.
const (
	scoreNameWord      = 3000 // term found at the start of a word in the name
	scoreNameSubstring = 2000 // term found inside a word in the name
	scoreNameFuzzy     = 1000 // term's letters appear in order in the name
	scoreField         = 100  // term found in another property
)

// Search ranks servers against a search string.
func Search(servers []links.Server, s string) []Hit { return Run(servers, Parse(s)) }

// Run ranks servers against a parsed query. With an empty query every
// server is returned in list order. Usable servers come before unusable
// ones at equal score.
func Run(servers []links.Server, q Query) []Hit {
	hits := make([]Hit, 0, len(servers))
	for i := range servers {
		if h, ok := match(&servers[i], q); ok {
			h.Pos = i
			hits = append(hits, h)
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score != hits[b].Score {
			return hits[a].Score > hits[b].Score
		}
		ua, ub := servers[hits[a].Pos].Usable(), servers[hits[b].Pos].Usable()
		if ua != ub {
			return ua
		}
		return hits[a].Pos < hits[b].Pos
	})
	return hits
}

func match(s *links.Server, q Query) (Hit, bool) {
	var h Hit
	for _, f := range q.Filters {
		if !strings.Contains(strings.ToLower(f.Field.Value(s)), f.Value) {
			return h, false
		}
	}
	name := []rune(strings.ToLower(s.Name))
	for _, t := range q.Terms {
		score, runes, field, ok := matchTerm(s, name, t)
		if !ok {
			return h, false
		}
		h.Score += score
		h.NameRunes = append(h.NameRunes, runes...)
		if h.Field == "" {
			h.Field = field
		}
	}
	h.NameRunes = dedupe(h.NameRunes)
	return h, true
}

func matchTerm(s *links.Server, name []rune, term string) (score int, runes []int, field string, ok bool) {
	t := []rune(term)
	if i := indexRunes(name, t); i >= 0 {
		score = scoreNameSubstring
		if i == 0 || !isWordRune(name[i-1]) {
			score = scoreNameWord
		}
		score -= min(i, 99) // earlier is better
		for k := range t {
			runes = append(runes, i+k)
		}
		return score, runes, "", true
	}
	if m := fuzzy.Find(term, []string{string(name)}); len(m) > 0 {
		runes := byteToRune(string(name), m[0].MatchedIndexes)
		// Letters scattered across the whole name are noise ("tls" in
		// "plaintext vless"); a typo keeps them close ("grmny" in "germany").
		if len(runes) > 0 && runes[len(runes)-1]-runes[0]+1 <= 2*len(t)+1 {
			score = scoreNameFuzzy + max(min(m[0].Score, 900), -900)
			return score, runes, "", true
		}
	}
	for _, f := range Fields {
		v := strings.ToLower(f.Value(s))
		if v == term {
			return scoreField + 50, nil, f.Name, true
		}
		if strings.Contains(v, term) {
			return scoreField, nil, f.Name, true
		}
	}
	return 0, nil, "", false
}

func indexRunes(s, sub []rune) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// byteToRune converts byte offsets in s to rune positions.
func byteToRune(s string, offsets []int) []int {
	pos := make(map[int]int, len(s))
	i := 0
	for b := range s {
		pos[b] = i
		i++
	}
	out := make([]int, 0, len(offsets))
	for _, o := range offsets {
		if r, ok := pos[o]; ok {
			out = append(out, r)
		}
	}
	return out
}

func dedupe(xs []int) []int {
	if len(xs) < 2 {
		return xs
	}
	sort.Ints(xs)
	out := xs[:1]
	for _, x := range xs[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}
