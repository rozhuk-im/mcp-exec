//go:build unix
// +build unix

package mcpxec_test

import (
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// The unix rows of the README platform matrix: setuid/setgid exist here, so the two
// settings are honoured, and the name lookups are tests here too — they need the
// system database, which is what this file is split for.

func TestUnixPrivDropSupported(t *testing.T) {
	if !mcpxec.PrivDropSupported() {
		t.Errorf("%s: setuid/setgid work here, so the settings must be honoured", runtime.GOOS)
	}
}

func TestResolveCredentialsByNameIsWhoTheProcessAlreadyIs(t *testing.T) {
	current, err := user.Current()
	if err != nil || current == nil {
		t.Skipf("this test needs the system database: %v", err)
	}

	uid, gid, err := mcpxec.ResolveCredentials(current.Username, "")
	if err != nil {
		t.Fatalf("the current user's own name must resolve, got %v", err)
	}

	// the name must answer the IDs the process already has — and a named user with no
	// named group brings its own primary group along, which is the pair the entry holds
	if want, _ := strconv.Atoi(current.Uid); uid != want {
		t.Errorf("the name must answer the uid of the same entry, got %d, the entry says %d", uid, want)
	}
	if want, _ := strconv.Atoi(current.Gid); gid != want {
		t.Errorf("a named user and no named group means the user's own group, got %d, the entry says %d", gid, want)
	}
}

func TestDropPrivilegesAskingToStayWhoYouAreIsNotAnError(t *testing.T) {
	configForTest(t)
	wantUID, wantGID := os.Getuid(), os.Getgid()
	mcpxec.Cfg.RunAsUser = strconv.Itoa(wantUID)
	mcpxec.Cfg.RunAsGroup = strconv.Itoa(wantGID)

	if err := mcpxec.DropPrivileges(); err != nil {
		t.Errorf("asking for the user and group you already are must not be an error, got %v", err)
	}
	if os.Getuid() != wantUID || os.Getgid() != wantGID {
		t.Errorf("nothing must have been applied, now uid=%d gid=%d, the test started as uid=%d gid=%d",
			os.Getuid(), os.Getgid(), wantUID, wantGID)
	}
}

func TestDropPrivilegesNeedsRootToChangeThem(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("the test runs as root here — a change is exactly what it asks for")
	}

	configForTest(t)
	mcpxec.Cfg.RunAsUser = "0" // a number, so no system database is needed
	mcpxec.Cfg.RunAsGroup = ""

	err := mcpxec.DropPrivileges()
	if err == nil {
		t.Fatal("a process that is not root cannot change its credentials; the startup must say so")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("the message must name the privilege that is missing, got %v", err)
	}
	// the setting was refused, not applied — the process is still itself
	if os.Getuid() == 0 || os.Getgid() == 0 {
		t.Errorf("nothing must have been applied, the process is uid=%d gid=%d", os.Getuid(), os.Getgid())
	}
}
