package main

import (
	"fmt"
	"os"
	"syscall"
)

// openLogFile opens the install log for appending without ever following a
// symlink, and makes sure it is a regular file owned by root (or by the
// current user when not root) with mode 0600, even when it already existed
// with looser permissions.
func openLogFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != 0 && int(st.Uid) != os.Geteuid() {
		f.Close()
		return nil, fmt.Errorf("%s is owned by user %d", path, st.Uid)
	}
	if fi.Mode().Perm() != 0o600 {
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}
