package service

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/AliSohani2082/sneakernet/internal/fsutil"
	"github.com/AliSohani2082/sneakernet/internal/layout"
	"github.com/AliSohani2082/sneakernet/internal/target"
)

const sysusersConf = `# Installed by sneakernet: the user the Xray service runs as.
u ` + layout.ServiceUser + ` - "Sneakernet Xray client" - -
`

// EnsureUser creates the service user with systemd-sysusers, which works
// offline and against another root (--root). The entry goes to
// /etc/sysusers.d, or to /run/sysusers.d when /etc/sysusers.d is read-only
// on the running system (NixOS). It returns the user's group id.
func EnsureUser(t target.Target, run Runner) (int, error) {
	if run == nil {
		run = execRunner
	}
	conf := layout.SysusersFile
	err := fsutil.WriteFile(t.Path(conf), []byte(sysusersConf), 0o644)
	if err != nil && t.Running && ReadOnly(err) {
		conf = layout.RuntimeSysusersFile
		err = fsutil.WriteFile(t.Path(conf), []byte(sysusersConf), 0o644)
	}
	if err != nil {
		return 0, err
	}
	args := []string{}
	if !t.Running {
		args = append(args, "--root="+t.Root)
	}
	if _, err := run("systemd-sysusers", append(args, t.Path(conf))...); err != nil {
		return 0, err
	}
	return GroupID(t, layout.ServiceUser)
}

// RemoveUser deletes the service user and its sysusers entry (best effort).
// On the running system userdel does it; in another root the account lines
// are dropped directly (userdel --root would need chroot privileges).
func RemoveUser(t target.Target, run Runner) {
	if run == nil {
		run = execRunner
	}
	if t.Running {
		_, _ = run("userdel", layout.ServiceUser)
	} else {
		for _, db := range []string{"/etc/passwd", "/etc/group", "/etc/shadow", "/etc/gshadow"} {
			_ = dropEntry(t.Path(db), layout.ServiceUser)
		}
	}
	for _, conf := range []string{layout.SysusersFile, layout.RuntimeSysusersFile} {
		if _, err := os.Lstat(t.Path(conf)); err == nil {
			_ = os.Remove(t.Path(conf))
		}
	}
}

// dropEntry removes the "name:..." line from an account database file.
func dropEntry(path, name string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	var kept []string
	for _, l := range strings.SplitAfter(string(b), "\n") {
		if !strings.HasPrefix(l, name+":") {
			kept = append(kept, l)
		}
	}
	return fsutil.WriteFile(path, []byte(strings.Join(kept, "")), fi.Mode().Perm())
}

// GroupID looks a group up in the target's /etc/group.
func GroupID(t target.Target, name string) (int, error) {
	f, err := os.Open(t.Path("/etc/group"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) >= 3 && fields[0] == name {
			return strconv.Atoi(fields[2])
		}
	}
	return 0, fmt.Errorf("group %q not found in %s", name, t.Path("/etc/group"))
}

// ChownToGroup gives path to root:gid. It only acts when running as root, so
// unprivileged test runs keep working.
func ChownToGroup(path string, gid int) error {
	if os.Geteuid() != 0 {
		return nil
	}
	return os.Chown(path, 0, gid)
}
