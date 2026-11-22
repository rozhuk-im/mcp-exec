//go:build dragonfly

package mcpxec

import (
	"os"
	"os/exec"
	"syscall"
)

// DragonFly BSD: the BSD capability set applies — chroot/setsid/Setpgid are exposed via
// syscall.SysProcAttr and applied in the child, so chroot isolation, setsid daemon mode
// and process-group kill work natively (chroot needs privileges). Only amd64 is
// supported by the Go toolchain on this platform.

func ChrootSupported() bool { return true }

// ProcAttr builds SysProcAttr with chroot and its own process group
func ProcAttr(chroot string, setpgid bool) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Chroot: chroot, Setpgid: setpgid}
}

// DaemonAttr detaches the daemon from the controlling terminal
func DaemonAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// KillChild kills the process group (children are started with Setpgid)
func KillChild(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill() // fall back to killing just the direct child
	}
	return nil
}

func PlatformSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGHUP, syscall.SIGTERM}
}
