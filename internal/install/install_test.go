package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AliSohani2082/sneakernet/internal/bundle"
	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/manage"
	"github.com/AliSohani2082/sneakernet/internal/service"
	"github.com/AliSohani2082/sneakernet/internal/target"
	"github.com/AliSohani2082/sneakernet/internal/xraytest"
)

// fakeBundle assembles a bundle from the real Xray build and the fixtures.
func fakeBundle(t *testing.T) *bundle.Bundle {
	t.Helper()
	xray, assets := xraytest.Binary(t)
	dir := t.TempDir()
	files := map[string]string{
		"bin/amd64/xray":       xray,
		"data/geoip.dat":       filepath.Join(assets, "geoip.dat"),
		"data/geosite.dat":     filepath.Join(assets, "geosite.dat"),
		"config/servers.txt":   "../../test/fixtures/servers.txt",
		"bin/amd64/sneakernet": "", // content does not matter here
	}
	var sums strings.Builder
	for rel, src := range files {
		data := []byte("#!/bin/sh\n")
		if src != "" {
			var err error
			if data, err = os.ReadFile(src); err != nil {
				t.Fatal(err)
			}
		}
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o755); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(data)
		sums.WriteString(hex.EncodeToString(h[:]) + "  " + rel + "\n")
	}
	os.WriteFile(filepath.Join(dir, "VERSION"), []byte("v0.1.0-test\n"), 0o644)
	os.WriteFile(filepath.Join(dir, bundle.SumsFile), []byte(sums.String()), 0o644)
	b, err := bundle.Open(dir, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Verify(); err != nil {
		t.Fatal(err)
	}
	return b
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestInstallIntoRootAndUninstall(t *testing.T) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("no systemctl")
	}
	b := fakeBundle(t)
	root := t.TempDir()
	tg := target.Dir(root)
	svc, err := service.For(tg, detect.Systemd)
	if err != nil {
		t.Fatal(err)
	}
	st := manage.DefaultState()
	st.Auto, st.Index = false, 3 // "vless ws tls" in the fixture

	res, err := Install(context.Background(), Options{Bundle: b, Target: tg, Svc: svc, State: st, Log: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Enabled || res.Started || res.State.Name != "vless ws tls" {
		t.Errorf("result: %+v", res)
	}
	if gid, err := service.GroupID(tg, layout.ServiceUser); err != nil || gid == 0 {
		t.Errorf("service user: gid=%d err=%v", gid, err)
	}

	for p, want := range map[string]os.FileMode{
		layout.XrayBin:                 0o755,
		layout.SelfBin:                 0o755,
		layout.ServersFile:             0o600,
		layout.ConfigFile:              0o640, // root:sneakernet
		layout.StateFile:               0o600,
		layout.EtcDir:                  0o750,
		layout.AssetDir + "/geoip.dat": 0o644,
	} {
		if got := mode(t, tg.Path(p)); got != want {
			t.Errorf("%s: mode %o, want %o", p, got, want)
		}
	}
	if dst, _ := os.Readlink(tg.Path(layout.CommandLink)); dst != layout.SelfBin {
		t.Errorf("command link -> %q", dst)
	}
	wants := tg.Path("/etc/systemd/system/multi-user.target.wants/" + layout.UnitName)
	if _, err := os.Lstat(wants); err != nil {
		t.Errorf("service not enabled: %v", err)
	}
	var cfg map[string]any
	raw, _ := os.ReadFile(tg.Path(layout.ConfigFile))
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	ob := cfg["outbounds"].([]any)[0].(map[string]any)
	if ob["streamSettings"].(map[string]any)["network"] != "ws" {
		t.Errorf("config does not use the chosen server: %v", ob)
	}

	// Switching servers rewrites the config; re-running install is idempotent.
	mgr := &manage.Manager{T: tg, Svc: svc}
	st2, err := mgr.State()
	if err != nil || st2.Index != 3 {
		t.Fatalf("saved state: %+v %v", st2, err)
	}
	if _, err := Install(context.Background(), Options{Bundle: b, Target: tg, Svc: svc, State: manage.DefaultState()}); err != nil {
		t.Fatalf("re-install: %v", err)
	}
	if st3, _ := mgr.State(); !st3.Auto {
		t.Errorf("re-install with auto state: %+v", st3)
	}

	if err := Uninstall(tg, svc, t.Logf); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GroupID(tg, layout.ServiceUser); err == nil {
		t.Errorf("service user still exists after uninstall")
	}
	for _, p := range []string{layout.OptDir, layout.EtcDir, layout.VarDir, layout.CommandLink, layout.UnitFile,
		layout.SysusersFile} {
		if _, err := os.Lstat(tg.Path(p)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after uninstall", p)
		}
	}
	if _, err := os.Lstat(wants); !os.IsNotExist(err) {
		t.Errorf("wants symlink still exists")
	}
}

func TestInstallWithoutServiceManager(t *testing.T) {
	b := fakeBundle(t)
	tg := target.Dir(t.TempDir())
	res, err := Install(context.Background(), Options{Bundle: b, Target: tg, State: manage.DefaultState()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Enabled || !strings.Contains(res.Manual, layout.XrayBin) {
		t.Errorf("result: %+v", res)
	}
	// No service user without systemd, so only root may read the config.
	if got := mode(t, tg.Path(layout.ConfigFile)); got != 0o600 {
		t.Errorf("config mode %o, want 600", got)
	}
}

func TestInstallRejectsUnusableServer(t *testing.T) {
	b := fakeBundle(t)
	tg := target.Dir(t.TempDir())
	st := manage.DefaultState()
	st.Auto, st.Index = false, 13 // the plaintext VLESS fixture
	if _, err := Install(context.Background(), Options{Bundle: b, Target: tg, State: st}); err == nil ||
		!strings.Contains(err.Error(), "plaintext") {
		t.Fatalf("want plaintext error, got %v", err)
	}
	if _, err := os.Stat(tg.Path(layout.ConfigFile)); !os.IsNotExist(err) {
		t.Error("config written despite the error")
	}
}

// A file at the command path that is not ours survives uninstall.
func TestUninstallKeepsForeignCommand(t *testing.T) {
	tg := target.Dir(t.TempDir())
	p := tg.Path(layout.CommandLink)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("someone else's"), 0o755)
	if err := Uninstall(tg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("foreign %s removed", layout.CommandLink)
	}
}
