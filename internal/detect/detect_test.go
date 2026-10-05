package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadOSFamilies(t *testing.T) {
	for _, tc := range []struct{ osRelease, family string }{
		{"NAME=\"Linux Mint\"\nID=linuxmint\nID_LIKE=\"ubuntu debian\"\nPRETTY_NAME=\"Linux Mint 22.3\"\n", "debian"},
		{"ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"26.04\"\n", "debian"},
		{"ID=fedora\nVERSION_ID=43\n", "fedora"},
		{"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", "fedora"},
		{"ID=cachyos\nID_LIKE=arch\n", "arch"},
		{"ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n", "suse"},
		{"ID=opensuse-leap\n", "suse"},
		{"ID=nixos\n", "nixos"},
		{"ID=alpine\n", "alpine"},
		{"ID=haiku\n", "other"},
	} {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "etc/os-release"), tc.osRelease)
		o, err := ReadOS(root)
		if err != nil || o.Family != tc.family {
			t.Errorf("%q: family=%q err=%v, want %q", tc.osRelease, o.Family, err, tc.family)
		}
	}
}

func TestReadOSFallbackAndMissing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "usr/lib/os-release"), "ID=debian\nPRETTY_NAME=\"Debian GNU/Linux 13\"\n")
	if o, err := ReadOS(root); err != nil || o.PrettyName != "Debian GNU/Linux 13" {
		t.Errorf("fallback: %+v %v", o, err)
	}
	if o, err := ReadOS(t.TempDir()); err == nil || o.Family != "other" {
		t.Errorf("missing: %+v %v", o, err)
	}
}

func TestInitSystemOnDisk(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "usr/lib/systemd/systemd"), "")
	if err := os.MkdirAll(filepath.Join(root, "sbin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../lib/systemd/systemd", filepath.Join(root, "sbin/init")); err != nil {
		t.Fatal(err)
	}
	if got := InitSystem(root, false); got != Systemd {
		t.Errorf("systemd root: %s", got)
	}

	root = t.TempDir()
	writeFile(t, filepath.Join(root, "sbin/openrc-run"), "")
	if got := InitSystem(root, false); got != OpenRC {
		t.Errorf("openrc root: %s", got)
	}
	if got := InitSystem(t.TempDir(), false); got != Unknown {
		t.Errorf("empty root: %s", got)
	}
}

func TestLiveCmdline(t *testing.T) {
	for cmdline, want := range map[string]bool{
		"BOOT_IMAGE=/casper/vmlinuz boot=casper quiet splash":              true,
		"initrd=x archisobasedir=arch archisolabel=ARCH_202609":            true,
		"root=live:CDLABEL=Fedora-WS-Live-43 rd.live.image quiet":          true,
		"BOOT_IMAGE=/@/boot/vmlinuz-linux-cachyos root=UUID=a221 rw quiet": false,
	} {
		if got := isLiveCmdline(cmdline); got != want {
			t.Errorf("%q: %v, want %v", cmdline, got, want)
		}
	}
}
