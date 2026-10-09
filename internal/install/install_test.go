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

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../test/fixtures/servers.txt")
	if err != nil {
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

	res, err := Install(context.Background(), Options{Bundle: b, Servers: fixture(t), Target: tg, Svc: svc, State: st, Log: t.Logf})
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
	if _, err := Install(context.Background(), Options{Bundle: b, Servers: fixture(t), Target: tg, Svc: svc, State: manage.DefaultState()}); err != nil {
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
	res, err := Install(context.Background(), Options{Bundle: b, Servers: fixture(t), Target: tg, State: manage.DefaultState()})
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
	if _, err := Install(context.Background(), Options{Bundle: b, Servers: fixture(t), Target: tg, State: st}); err == nil ||
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

// Without usable servers the install still completes, saves the settings
// and leaves the service off until servers are added.
func TestInstallWithoutServers(t *testing.T) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("no systemctl")
	}
	b := fakeBundle(t)
	tg := target.Dir(t.TempDir())
	svc, err := service.For(tg, detect.Systemd)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Install(context.Background(), Options{Bundle: b, Target: tg, Svc: svc,
		State: manage.DefaultState(), Servers: []byte("# empty\n")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.NoServers || res.Enabled || res.Started {
		t.Errorf("result: %+v", res)
	}
	mgr := &manage.Manager{T: tg, Svc: svc}
	if mgr.Configured() || !mgr.Installed() {
		t.Errorf("configured=%v installed=%v", mgr.Configured(), mgr.Installed())
	}
	if _, err := mgr.State(); err != nil {
		t.Errorf("state not saved: %v", err)
	}
	if _, err := os.Lstat(tg.Path("/etc/systemd/system/multi-user.target.wants/" + layout.UnitName)); !os.IsNotExist(err) {
		t.Error("service enabled without servers")
	}
}

func TestPickCommandDir(t *testing.T) {
	only := func(ok ...string) func(string) bool {
		return func(d string) bool {
			for _, o := range ok {
				if d == o {
					return true
				}
			}
			return false
		}
	}
	for name, tc := range map[string]struct {
		path        string
		preferLocal bool
		usable      func(string) bool
		dir         string
		onPath      bool
	}{
		"debian sudo": {"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", true,
			only(), "/usr/local/bin", true},
		"nixos": {"/run/wrappers/bin:/root/.nix-profile/bin:/nix/var/nix/profiles/default/bin:/run/current-system/sw/bin",
			false, only("/run/wrappers/bin"), "/run/wrappers/bin", true},
		// NixOS sudo may still hand over a FHS-style PATH; /usr/local/bin is not on a user's PATH there.
		"nixos with fhs sudo path": {"/run/wrappers/bin:/usr/local/sbin:/usr/local/bin:/usr/bin", false,
			only("/run/wrappers/bin", "/usr/local/sbin"), "/run/wrappers/bin", true},
		"skips package dirs and homes": {"/home/u/.local/bin:/usr/bin:/opt/tools/bin", true,
			only("/home/u/.local/bin", "/usr/bin", "/opt/tools/bin"), "/opt/tools/bin", true},
		"nothing usable": {"/run/current-system/sw/bin", false, only(), "/usr/local/bin", false},
	} {
		dir, onPath := pickCommandDir(tc.path, tc.preferLocal, tc.usable)
		if dir != tc.dir || onPath != tc.onPath {
			t.Errorf("%s: got %s %v, want %s %v", name, dir, onPath, tc.dir, tc.onPath)
		}
	}
}

func TestCommandDirOnDiskTargets(t *testing.T) {
	if dir, _ := commandDir(target.Dir("/mnt/x"), "nixos"); dir != "" {
		t.Errorf("nixos disk target: %q", dir)
	}
	if dir, onPath := commandDir(target.Dir("/mnt/x"), "debian"); dir != "/usr/local/bin" || !onPath {
		t.Errorf("debian disk target: %q %v", dir, onPath)
	}
}

// dummyBundle is a complete bundle with placeholder contents, for tests that
// fail before Xray would run (so they need no Xray build).
func dummyBundle(t *testing.T) *bundle.Bundle {
	t.Helper()
	dir := t.TempDir()
	var sums strings.Builder
	for _, rel := range []string{"bin/amd64/xray", "bin/amd64/sneakernet", "data/geoip.dat", "data/geosite.dat"} {
		data := []byte("content of " + rel)
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, data, 0o755); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(data)
		sums.WriteString(hex.EncodeToString(h[:]) + "  " + rel + "\n")
	}
	os.WriteFile(filepath.Join(dir, bundle.SumsFile), []byte(sums.String()), 0o644)
	b, err := bundle.Open(dir, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestInstallRefusesSymlinkedTargetDirectory(t *testing.T) {
	for _, link := range []string{"opt", "etc", "var"} {
		host := t.TempDir() // stands in for the real /opt, /etc, /var
		root := t.TempDir()
		if err := os.Symlink(host, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
		_, err := Install(context.Background(), Options{
			Bundle: dummyBundle(t), Target: target.Dir(root), State: manage.DefaultState(), Servers: nil,
		})
		if err == nil {
			t.Fatalf("%s symlinked out of the target: install succeeded", link)
		}
		if ents, _ := os.ReadDir(host); len(ents) != 0 {
			t.Errorf("%s: wrote outside the target root: %v", link, ents)
		}
	}
}

func TestInstallRefusesSymlinkedManagedDirectory(t *testing.T) {
	// <root>/opt/sneakernet is a symlink to another place inside the root.
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "opt", "elsewhere"), 0o755)
	if err := os.Symlink("elsewhere", filepath.Join(root, "opt", "sneakernet")); err != nil {
		t.Fatal(err)
	}
	_, err := Install(context.Background(), Options{Bundle: dummyBundle(t), Target: target.Dir(root), State: manage.DefaultState()})
	if err == nil || !strings.Contains(err.Error(), "not a plain directory") {
		t.Fatalf("want a clear refusal, got %v", err)
	}
}

func TestInstallRefusesWritableManagedDirectory(t *testing.T) {
	for _, mode := range []os.FileMode{0o775, 0o757} {
		root := t.TempDir()
		bin := filepath.Join(root, "opt/sneakernet/bin")
		os.MkdirAll(bin, 0o755)
		os.Chmod(bin, mode)
		_, err := Install(context.Background(), Options{Bundle: dummyBundle(t), Target: target.Dir(root), State: manage.DefaultState()})
		if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
			t.Fatalf("mode %o: want a clear refusal, got %v", mode, err)
		}
		if _, err := os.Stat(filepath.Join(bin, "xray")); err == nil {
			t.Errorf("mode %o: a binary was installed into the unsafe directory", mode)
		}
	}
}

func TestInstallRejectsIncompleteBundleBeforeTouchingFiles(t *testing.T) {
	b := dummyBundle(t)
	sums, _ := os.ReadFile(filepath.Join(b.Dir, bundle.SumsFile))
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		if !strings.HasSuffix(l, "bin/amd64/xray") {
			keep = append(keep, l)
		}
	}
	os.WriteFile(filepath.Join(b.Dir, bundle.SumsFile), []byte(strings.Join(keep, "\n")+"\n"), 0o644)
	root := t.TempDir()
	_, err := Install(context.Background(), Options{Bundle: b, Target: target.Dir(root), State: manage.DefaultState()})
	if err == nil || !strings.Contains(err.Error(), "bin/amd64/xray") {
		t.Fatalf("want an incomplete-bundle error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "opt/sneakernet/bin/sneakernet")); err == nil {
		t.Error("files were copied from an incomplete bundle")
	}
}

func TestInstallCopiesStagedBytesNotLaterStickContents(t *testing.T) {
	b := dummyBundle(t)
	// The stick is changed after Open/Verify but before Install: it must fail.
	os.WriteFile(filepath.Join(b.Dir, "bin/amd64/sneakernet"), []byte("replaced on the stick"), 0o755)
	root := t.TempDir()
	if _, err := Install(context.Background(), Options{Bundle: b, Target: target.Dir(root), State: manage.DefaultState()}); err == nil {
		t.Fatal("install accepted a file that no longer matches SHA256SUMS")
	}
	if got, err := os.ReadFile(filepath.Join(root, "opt/sneakernet/bin/sneakernet")); err == nil {
		t.Errorf("mutated binary installed: %q", got)
	}
}
