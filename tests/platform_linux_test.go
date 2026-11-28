//go:build linux

package mcpxec_test

import (
	"os"
	"syscall"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// The Linux row of the README platform matrix: chroot, setuid/setgid, setsid, group kill

func TestLinuxChrootSupported(t *testing.T) {
	if !mcpxec.ChrootSupported() {
		t.Error("linux: chroot must be supported")
	}
}

func TestLinuxPrivDropSupported(t *testing.T) {
	if !mcpxec.PrivDropSupported() {
		t.Error("linux: setuid/setgid are what dropping to a user needs, they must be supported")
	}
}

func TestLinuxProcAttr(t *testing.T) {
	a := mcpxec.ProcAttr("/chroot-test", true)
	if a == nil {
		t.Fatal("nil SysProcAttr")
	}
	if a.Chroot != "/chroot-test" {
		t.Errorf("Chroot must be applied, got %q", a.Chroot)
	}
	if !a.Setpgid {
		t.Error("Setpgid must be set — process-group kill on timeout depends on it")
	}
}

func TestLinuxDaemonAttr(t *testing.T) {
	a := mcpxec.DaemonAttr()
	if a == nil {
		t.Fatal("nil SysProcAttr")
	}
	if !a.Setsid {
		t.Error("daemon mode must detach from the terminal via Setsid")
	}
}

func TestLinuxSignals(t *testing.T) {
	sigs := mcpxec.PlatformSignals()
	if !hasSignal(sigs, os.Interrupt) {
		t.Error("SIGINT must be handled")
	}
	if !hasSignal(sigs, syscall.SIGHUP) {
		t.Error("SIGHUP must be part of the graceful-shutdown set")
	}
	if !hasSignal(sigs, syscall.SIGTERM) {
		t.Error("SIGTERM must be handled")
	}
}
