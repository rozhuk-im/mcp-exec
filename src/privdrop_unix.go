//go:build unix
// +build unix

package mcpxec

import (
	"fmt"
	"os"
	"syscall"
)

// applyCredentials sets the group list, the group and the user, in that order —
// setuid() comes last because it is the call that takes away the privilege the
// other two need. The supplementary group list becomes the single group the
// process drops to: a daemon does not keep the supplementary groups of the user it
// started as, which are root's when the drop starts from root.
func applyCredentials(uid, gid int) error {
	if gid != os.Getgid() {
		if err := syscall.Setgroups([]int{gid}); err != nil {
			return fmt.Errorf("setgroups: %w", err)
		}
		if err := syscall.Setgid(gid); err != nil {
			return fmt.Errorf("setgid: %w", err)
		}
	}
	if uid != os.Getuid() {
		if err := syscall.Setuid(uid); err != nil {
			return fmt.Errorf("setuid: %w", err)
		}
	}

	return nil
}
