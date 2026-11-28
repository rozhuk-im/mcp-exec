//go:build darwin

package mcpxec

import (
	"os"
	"os/exec"
	"syscall"
)

// chroot() is not used on darwin by design (see the README platform matrix):
// SysProcAttr.Chroot exists here, but macOS prefers setuid/setgid/launchd isolation.
func ChrootSupported() bool { return false }

// PrivDropSupported reports that setuid/setgid are exactly what macOS does have, so
// [security] user/group are honoured here — chroot is not (the comment above)
func PrivDropSupported() bool { return true }

func ProcAttr(chroot string, setpgid bool) *syscall.SysProcAttr {
	_ = chroot
	return &syscall.SysProcAttr{Setpgid: setpgid}
}

func DaemonAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func KillChild(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

func PlatformSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGHUP, syscall.SIGTERM}
}
