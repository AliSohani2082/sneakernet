// Package target abstracts where sneakernet installs: the running system, or
// another root filesystem (an installed system on disk, or a test directory).
package target

import "path/filepath"

// Target is a root filesystem to install into.
type Target struct {
	// Root is "/" for the running system.
	Root string
	// Running means the target is the booted system, so services can be
	// started now. For any other root they are only enabled for next boot.
	Running bool
}

// RunningSystem is the booted system.
func RunningSystem() Target { return Target{Root: "/", Running: true} }

// Dir is another root filesystem, for example an installed system mounted
// at /mnt/target.
func Dir(root string) Target { return Target{Root: root} }

// Path maps an absolute path in the target to a host path.
func (t Target) Path(p string) string { return filepath.Join(t.Root, p) }
