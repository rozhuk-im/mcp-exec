//go:build windows

package mcpxec

import (
	"os"
	"os/exec"
	"syscall"
)

// chroot is not available on Windows
func ChrootSupported() bool { return false }

// PrivDropSupported reports that there is no setuid/setgid here, so [security] user
// and group are refused at startup (see src/privdrop_windows_stub.go)
func PrivDropSupported() bool { return false }

func ProcAttr(chroot string, setpgid bool) *syscall.SysProcAttr {
	return nil
}

func DaemonAttr() *syscall.SysProcAttr {
	return nil
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
