//go:build freebsd || netbsd || openbsd || dragonfly

package mcpxec_test

import (
	"os"
	"runtime"
	"syscall"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// The BSD row of the README platform matrix. Go's BSD syscall package exposes
// Chroot/Setpgid/Setsid in SysProcAttr and applies chroot() in the child, so FreeBSD,
// DragonFly BSD, OpenBSD and NetBSD get the same feature level as Linux: chroot
// isolation (needs privileges), setsid daemon mode, process-group kill, SIGHUP and the
// setuid/setgid [security] user and group drop.

func TestBSDChrootSupported(t *testing.T) {
	if !mcpxec.ChrootSupported() {
		t.Errorf("%s: chroot must be supported on the BSDs", runtime.GOOS)
	}
}

func TestBSDPrivDropSupported(t *testing.T) {
	if !mcpxec.PrivDropSupported() {
		t.Errorf("%s: setuid/setgid work on the BSDs, they must be supported", runtime.GOOS)
	}
}

func TestBSDProcAttr(t *testing.T) {
	a := mcpxec.ProcAttr("/chroot-test", true)
	if a == nil {
		t.Fatal("nil SysProcAttr")
	}
	if a.Chroot != "/chroot-test" {
		t.Errorf("%s: Chroot must be applied, got %q", runtime.GOOS, a.Chroot)
	}
	if !a.Setpgid {
		t.Error("Setpgid must be set — process-group kill on timeout depends on it")
	}
}

func TestBSDDaemonAttr(t *testing.T) {
	a := mcpxec.DaemonAttr()
	if a == nil {
		t.Fatal("nil SysProcAttr")
	}
	if !a.Setsid {
		t.Errorf("%s: daemon mode must detach from the terminal via Setsid", runtime.GOOS)
	}
}

func TestBSDSignals(t *testing.T) {
	sigs := mcpxec.PlatformSignals()
	if !hasSignal(sigs, os.Interrupt) {
		t.Error("SIGINT must be handled")
	}
	if !hasSignal(sigs, syscall.SIGHUP) {
		t.Errorf("%s: SIGHUP must be part of the graceful-shutdown set", runtime.GOOS)
	}
	if !hasSignal(sigs, syscall.SIGTERM) {
		t.Error("SIGTERM must be handled")
	}
}
