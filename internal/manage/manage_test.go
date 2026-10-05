package manage

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/target"
)

const (
	a = "trojan://pw@a.example.com:443#A"
	b = "trojan://pw@b.example.com:443#B"
	c = "trojan://pw@c.example.com:443#C"
)

func withList(t *testing.T, content string) *Manager {
	t.Helper()
	m := &Manager{T: target.Dir(t.TempDir())}
	if content != "" {
		p := m.T.Path(layout.ServersFile)
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return m
}

func list(t *testing.T, m *Manager) string {
	t.Helper()
	b, _ := os.ReadFile(m.T.Path(layout.ServersFile))
	return string(b)
}

func TestAddCreatesFileAndSkipsDuplicates(t *testing.T) {
	m := withList(t, "")
	if s, _, err := m.Servers(); err != nil || len(s) != 0 {
		t.Fatalf("missing list should be empty: %v %v", s, err)
	}
	plan, err := m.AddLinks(a + "\n\n" + b + "\nnot a link\n" + a)
	if err != nil || len(plan.New) != 2 || plan.Duplicates != 1 || len(plan.Errors) != 1 {
		t.Fatalf("plan %+v err %v", plan, err)
	}
	plan, _ = m.AddLinks(b + "\n" + c)
	if len(plan.New) != 1 || plan.Duplicates != 1 {
		t.Errorf("second add: %+v", plan)
	}
	if got := list(t, m); got != a+"\n"+b+"\n"+c+"\n" {
		t.Errorf("list:\n%s", got)
	}
	if fi, _ := os.Stat(m.T.Path(layout.ServersFile)); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
}

func TestRemoveKeepsComments(t *testing.T) {
	m := withList(t, "# my servers\r\n"+a+"\r\n# backup\r\n"+b+"\r\n") // edited on Windows
	servers, _, _ := m.Servers()
	if _, err := m.RemoveServer(servers[0].Key()); err != nil {
		t.Fatal(err)
	}
	if got := list(t, m); got != "# my servers\r\n# backup\r\n"+b+"\r\n" {
		t.Errorf("list:\n%q", got)
	}
	if _, err := m.RemoveServer("nope"); err == nil {
		t.Error("unknown key: want error")
	}
}

func TestSubscriptionFileBecomesPlainOnEdit(t *testing.T) {
	m := withList(t, base64.StdEncoding.EncodeToString([]byte(a+"\n"+b+"\n")))
	if _, err := m.AddLinks(c); err != nil {
		t.Fatal(err)
	}
	if got := list(t, m); got != a+"\n"+b+"\n"+c+"\n" {
		t.Errorf("list:\n%s", got)
	}
}

func TestStateKeySurvivesReorder(t *testing.T) {
	m := withList(t, a+"\n"+b+"\n")
	servers, _, _ := m.Servers()
	st := DefaultState()
	st.Use(&servers[1]) // B, index 2
	if _, err := m.RemoveServer(servers[0].Key()); err != nil {
		t.Fatal(err)
	}
	servers, _, _ = m.Servers()
	got, err := resolve(servers, st)
	if err != nil || got.Index != 1 || got.Name != "B" {
		t.Errorf("resolve after removal: %+v %v", got, err)
	}
	st.Key = "gone"
	if _, err := resolve(servers, st); err == nil || !strings.Contains(err.Error(), "no longer in the list") {
		t.Errorf("missing server: %v", err)
	}
}
