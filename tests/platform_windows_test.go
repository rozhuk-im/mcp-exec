//go:build windows
// +build windows

package mcpxec_test

import (
	"os"
	"syscall"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// The Windows row of the README platform matrix: no chroot, no SysProcAttr support
// (both attrs stay nil), no syslog, and the signal set is SIGINT + SIGTERM only.

func TestWindowsChrootSupported(t *testing.T) {
	if mcpxec.ChrootSupported() {
		t.Error("windows: chroot must stay unsupported")
	}
}

func TestWindowsProcAttr(t *testing.T) {
	if a := mcpxec.ProcAttr("/chroot-test", true); a != nil {
		t.Errorf("windows: ProcAttr must stay nil, got %+v", a)
	}
}

func TestWindowsDaemonAttr(t *testing.T) {
	if a := mcpxec.DaemonAttr(); a != nil {
		t.Errorf("windows: DaemonAttr must stay nil (no setsid), got %+v", a)
	}
}

func TestWindowsSignals(t *testing.T) {
	sigs := mcpxec.PlatformSignals()
	if len(sigs) != 2 {
		t.Errorf("windows: expected SIGINT + SIGTERM, got %v", sigs)
	}
	if !hasSignal(sigs, os.Interrupt) {
		t.Error("SIGINT must be handled")
	}
	if !hasSignal(sigs, syscall.SIGTERM) {
		t.Error("SIGTERM must be handled")
	}
}
