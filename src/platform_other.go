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

// PrivDropSupported reports false for these ports, as ChrootSupported above does —
// the platforms the platform layer does not name are the untested ones, so setting
// [security] user or group here stops the startup (see privdrop_other.go)
func PrivDropSupported() bool { return false }

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
