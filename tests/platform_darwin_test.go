//go:build darwin

package mcpxec_test

import (
	"os"
	"syscall"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// The macOS row of the README platform matrix: setsid/SIGHUP/group kill work,
// chroot stays disabled by design (SysProcAttr.Chroot exists on darwin, but the
// project does not use it — see src/platform_darwin.go).

func TestDarwinChrootSupported(t *testing.T) {
	if mcpxec.ChrootSupported() {
		t.Error("darwin: chroot is intentionally not used")
	}
}

func TestDarwinProcAttr(t *testing.T) {
	a := mcpxec.ProcAttr("/chroot-test", true)
	if a == nil {
		t.Fatal("nil SysProcAttr")
	}
	if a.Chroot != "" {
		t.Errorf("chroot must stay ignored on darwin, got %q", a.Chroot)
	}
	if !a.Setpgid {
		t.Error("Setpgid must be set — group kill depends on it")
	}
}

func TestDarwinDaemonAttr(t *testing.T) {
	a := mcpxec.DaemonAttr()
	if a == nil {
		t.Fatal("nil SysProcAttr")
	}
	if !a.Setsid {
		t.Error("daemon mode must detach from the terminal via Setsid")
	}
}

func TestDarwinSignals(t *testing.T) {
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
