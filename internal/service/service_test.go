package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AliSohani2082/sneakernet/internal/detect"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/target"
)

type recorder struct{ calls []string }

func (r *recorder) run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if len(args) > 0 && args[0] == "show" {
		return []byte("ActiveState=active\nSubState=running\nActiveEnterTimestamp=Sun 2026-10-04 20:00:00 +0330\nUnitFileState=enabled\n"), nil
	}
	return nil, nil
}

func TestRunningSystemCommands(t *testing.T) {
	root := t.TempDir()
	rec := &recorder{}
	s := &Systemd{T: target.Target{Root: root, Running: true}, Run: rec.run}
	if err := s.Install(layout.UnitName, XrayUnit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Enable(layout.UnitName); err != nil {
		t.Fatal(err)
	}
	st, err := s.Status(layout.UnitName)
	if err != nil || !st.Active || !st.Enabled || st.State != "active (running)" {
		t.Errorf("status: %+v %v", st, err)
	}
	if err := s.Remove(layout.UnitName); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"systemctl daemon-reload",
		"systemctl enable --now " + layout.UnitName,
		"systemctl show " + layout.UnitName + " --property=ActiveState,SubState,ActiveEnterTimestamp,UnitFileState",
		"systemctl disable --now " + layout.UnitName,
		"systemctl daemon-reload",
		"systemctl reset-failed " + layout.UnitName,
	}
	if strings.Join(rec.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(rec.calls, "\n"), strings.Join(want, "\n"))
	}
	if _, err := os.Stat(filepath.Join(root, layout.UnitFile)); !os.IsNotExist(err) {
		t.Errorf("unit file not removed: %v", err)
	}
}

func TestOtherRootOnlyEnables(t *testing.T) {
	root := t.TempDir()
	rec := &recorder{}
	s := &Systemd{T: target.Dir(root), Run: rec.run}
	if err := s.Install(layout.UnitName, XrayUnit()); err != nil {
		t.Fatal(err)
	}
	_ = s.Enable(layout.UnitName)
	_ = s.Restart(layout.UnitName)
	if got := strings.Join(rec.calls, "\n"); got != "systemctl --root="+root+" enable "+layout.UnitName {
		t.Errorf("calls: %s", got)
	}
}

// TestRealSystemctlEnablesOffline runs the real `systemctl --root` against a
// temp dir: it must create the multi-user.target.wants symlink without
// touching the host.
func TestRealSystemctlEnablesOffline(t *testing.T) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("no systemctl")
	}
	root := t.TempDir()
	m, err := For(target.Dir(root), detect.Systemd)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Install(layout.UnitName, XrayUnit()); err != nil {
		t.Fatal(err)
	}
	if err := m.Enable(layout.UnitName); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "etc/systemd/system/multi-user.target.wants", layout.UnitName)
	if dst, err := os.Readlink(link); err != nil || dst != layout.UnitFile {
		t.Errorf("wants symlink: %q %v", dst, err)
	}
	if err := m.Remove(layout.UnitName); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("wants symlink left behind: %v", err)
	}
}

func TestUnitMatchesLayout(t *testing.T) {
	u := string(XrayUnit())
	for _, want := range []string{
		"User=" + layout.ServiceUser,
		"ExecStart=" + layout.XrayBin + " run -config " + layout.ConfigFile,
		"XRAY_LOCATION_ASSET=" + layout.AssetDir,
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit is missing %q", want)
		}
	}
}

// TestUnitVerifies runs systemd-analyze on the unit inside a fake root that
// has the binary it points to.
func TestUnitVerifies(t *testing.T) {
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		t.Skip("no systemd-analyze")
	}
	root := t.TempDir()
	bin := filepath.Join(root, layout.XrayBin)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(root, layout.UnitFile)
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, XrayUnit(), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("systemd-analyze", "verify", "--root="+root, unit).CombinedOutput(); err != nil {
		t.Fatalf("systemd-analyze verify: %v\n%s", err, out)
	}
}

func TestUnsupportedInit(t *testing.T) {
	if _, err := For(target.RunningSystem(), detect.OpenRC); err == nil {
		t.Error("openrc: want error")
	}
}
