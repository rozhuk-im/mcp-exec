//go:build openbsd

package mcpxec

import (
	"os"
	"os/exec"
	"syscall"
)

// OpenBSD: chroot/setsid/Setpgid come from syscall.SysProcAttr and are applied in the
// child before exec, so chroot isolation, setsid daemon mode and process-group kill
// work natively. chroot() needs privileges; if you want the daemon to drop to an
// unprivileged user, do it with setuid/setgid at startup (SysProcAttr.Credential is
// not used by this project), not by chrooting the command itself.

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
