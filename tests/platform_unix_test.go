//go:build unix
// +build unix

package mcpxec_test

import (
	"os/exec"
	"testing"
	"time"

	mcpxec "github.com/mcp-exec/src"
)

// Children are started in their own process group (Setpgid), so a timeout kills the
// whole group — on Linux and the BSDs KillChild signals -pid, elsewhere it falls back
// to the direct child. Either way the child must die and be reaped promptly.

func TestKillChildProcessGroup(t *testing.T) {
	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not available on this platform")
	}

	cmd := exec.Command(path, "60")
	cmd.SysProcAttr = mcpxec.ProcAttr("", true)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := mcpxec.KillChild(cmd); err != nil {
		t.Errorf("KillChild: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }() // Wait returns an error for a killed process — expected

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the child was not reaped after KillChild")
	}
}
