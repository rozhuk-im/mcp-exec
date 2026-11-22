//go:build !linux && !darwin && !windows && !freebsd && !netbsd && !openbsd && !dragonfly
// +build !linux,!darwin,!windows,!freebsd,!netbsd,!openbsd,!dragonfly

package mcpxec

import (
	"os"
	"os/exec"
	"syscall"
)

// Generic fallback for the remaining platforms (aix, solaris/illumos, z/OS, plan9, js/wasm...).
// FreeBSD/DragonFlyBSD/OpenBSD/NetBSD have their own files; chroot() is not exposed
// through SysProcAttr here, so setting chroot_path on these platforms stays fatal.
// Exotic ports are not tested — verify with `GOOS=<os> go build ./cmd/mcp-exec` before use.

func ChrootSupported() bool { return false }

func ProcAttr(chroot string, setpgid bool) *syscall.SysProcAttr {
	_ = chroot
	return &syscall.SysProcAttr{Setpgid: setpgid}
}

func DaemonAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func KillChild(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func PlatformSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
