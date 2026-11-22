package mcpxec_test

import (
	"os"
	"os/exec"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// Platform-shape tests are split per build tag (platform_<os>_test.go), because
// per-platform SysProcAttr fields (Chroot/Setpgid/Setsid) exist only on the platforms
// that have them; this file holds the shared bits and the always-safe checks.

// hasSignal reports whether sigs contains want.
func hasSignal(sigs []os.Signal, want os.Signal) bool {
	for _, s := range sigs {
		if s == want {
			return true
		}
	}
	return false
}

func TestKillChildNilProcess(t *testing.T) {
	// A command that was never started has no process to kill
	if err := mcpxec.KillChild(&exec.Cmd{}); err != nil {
		t.Errorf("expected nil error for a not-started command, got %v", err)
	}
}

func TestSyslogPriority(t *testing.T) {
	// LOG_INFO == 6 on every platform that has log/syslog (Linux, the BSDs, macOS);
	// the stubs mirror the same constant, so the startup path stays uniform.
	if mcpxec.SyslogLogInfo != 6 {
		t.Errorf("unexpected syslog priority %d", mcpxec.SyslogLogInfo)
	}
}
