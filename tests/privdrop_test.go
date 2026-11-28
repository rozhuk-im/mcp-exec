package mcpxec_test

import (
	"os"
	"strings"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// Tests for [security] user and group — the credentials the process drops to. The
// portable part is here: the two settings reach the config from the section and from
// the env override, a value of all digits is an ID and needs no lookup, a name the
// system does not know is refused, and Run stops on it before a request is served.
// The name lookups — which need a system database, and answer SIDs on Windows rather
// than numbers — are in tests/privdrop_unix_test.go.

func TestSecuritySectionReadsUserAndGroup(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_USER", "")  // pin the env overrides so the assertions below hold
	t.Setenv("MCP_EXEC_GROUP", "") // the same
	path := iniFile(t, "[server]\nport = 9999\n\n[security]\nchroot_path = \nuser = someuser\ngroup = 100\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if mcpxec.Cfg.Port != 9999 {
		t.Errorf("the new keys must not confuse the reader, port should stay 9999, got %d", mcpxec.Cfg.Port)
	}
	if mcpxec.Cfg.RunAsUser != "someuser" {
		t.Errorf("[security] user must reach the config, got %q", mcpxec.Cfg.RunAsUser)
	}
	if mcpxec.Cfg.RunAsGroup != "100" {
		t.Errorf("[security] group must reach the config, got %q", mcpxec.Cfg.RunAsGroup)
	}
}

func TestEnvUserAndGroupOverrideTheSection(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_USER", "env-user")
	t.Setenv("MCP_EXEC_GROUP", "env-group")
	path := iniFile(t, "[security]\nuser = section-user\ngroup = section-group\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if mcpxec.Cfg.RunAsUser != "env-user" {
		t.Errorf("MCP_EXEC_USER beats the section, got %q", mcpxec.Cfg.RunAsUser)
	}
	if mcpxec.Cfg.RunAsGroup != "env-group" {
		t.Errorf("MCP_EXEC_GROUP beats the section the same way, got %q", mcpxec.Cfg.RunAsGroup)
	}
}

func TestResolveCredentialsWithoutSettingsKeepsWhatTheProcessIs(t *testing.T) {
	uid, gid, err := mcpxec.ResolveCredentials("", "")
	if err != nil {
		t.Fatalf("two empty settings must not be an error, got %v", err)
	}
	if uid != os.Getuid() || gid != os.Getgid() {
		t.Errorf("an empty setting means stay what you are, got uid=%d gid=%d, the process is uid=%d gid=%d",
			uid, gid, os.Getuid(), os.Getgid())
	}
}

func TestResolveCredentialsTakesPlainNumbersAsIDs(t *testing.T) {
	// no lookup here, so no system database is needed — which is what makes this test
	// the same on every platform
	uid, gid, err := mcpxec.ResolveCredentials("4222", "4321")
	if err != nil {
		t.Fatalf("a plain number is an ID: %v", err)
	}
	if uid != 4222 || gid != 4321 {
		t.Errorf("expected uid=4222 gid=4321, got uid=%d gid=%d", uid, gid)
	}

	// one setting empty keeps what the process already is
	uid, gid, err = mcpxec.ResolveCredentials("4222", "")
	if err != nil || uid != 4222 || gid != os.Getgid() {
		t.Errorf("the group alone must stay the process's own, got uid=%d gid=%d err=%v", uid, gid, err)
	}

	uid, gid, err = mcpxec.ResolveCredentials("", "4321")
	if err != nil || uid != os.Getuid() || gid != 4321 {
		t.Errorf("the user alone must stay the process's own, got uid=%d gid=%d err=%v", uid, gid, err)
	}
}

func TestResolveCredentialsRefusesNamesItCannotFind(t *testing.T) {
	_, _, err := mcpxec.ResolveCredentials("no-such-user-9f3c2a", "")
	if err == nil {
		t.Fatal("a name the database does not know must be refused, not dropped")
	}
	if !strings.Contains(err.Error(), "user") {
		t.Errorf("the message must name the setting it refused, got %v", err)
	}

	_, _, err = mcpxec.ResolveCredentials("", "no-such-group-9f3c2a")
	if err == nil {
		t.Fatal("the group setting goes through the same rule")
	}
	if !strings.Contains(err.Error(), "group") {
		t.Errorf("the message must name the setting it refused, got %v", err)
	}
}

func TestRunStopsBeforeASettingItCannotHonour(t *testing.T) {
	configForTest(t) // Run rebuilds the shared config; configForTest puts it back
	wantUID, wantGID := os.Getuid(), os.Getgid()

	// stdio, so the test stops at the drop itself: no log file is opened, no port is
	// bound, nothing listens — Run returns the reason before a request is served
	t.Setenv("MCP_EXEC_USER", "no-such-user-9f3c2a")
	t.Setenv("MCP_EXEC_GROUP", "")
	path := iniFile(t, "[server]\ntransport = stdio\n\n[security]\nuser = \n")

	err := mcpxec.Run("", path)
	if err == nil {
		t.Fatal("a user the system does not know must stop the startup")
	}
	// on the unix platforms this is the lookup refusing the name, on Windows it is the
	// platform check — both messages name the setting they refused
	if !strings.Contains(err.Error(), "user") {
		t.Errorf("the message must name the setting it refused, got %v", err)
	}
	// and the process is still itself: the setting was refused, not applied
	if os.Getuid() != wantUID || os.Getgid() != wantGID {
		t.Errorf("nothing must have been applied, uid=%d gid=%d, the test started as uid=%d gid=%d",
			os.Getuid(), os.Getgid(), wantUID, wantGID)
	}
}
