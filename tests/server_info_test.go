package mcpxec_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// Tests for the name the initialize answer reports: [server] name, the hostname
// when it is empty, a generated DNS label when the hostname cannot be read, the env
// override beating the section, the validation both the section and the env var go
// through — an invalid name stops the startup — plus the title and the description,
// both of which carry that same name.

// initializeAnswer runs an initialize round trip and hands back the serverInfo
// block as the client sees it — the map is marshaled and read back, rather than
// reached into as Go built it.
func initializeAnswer(t *testing.T) map[string]any {
	t.Helper()

	resp := mcpxec.HandleRequest(&mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("expected an initialize result, got a JSON-RPC error: %v", resp.Error)
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Result struct {
			ServerInfo map[string]any `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return decoded.Result.ServerInfo
}

func TestServerNameComesFromTheSection(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_NAME", "") // pin the env override so the assertions below hold
	path := iniFile(t, "[server]\nname = srv.example.com\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if mcpxec.Cfg.Name != "srv.example.com" {
		t.Errorf("[server] name must reach the config, got %q", mcpxec.Cfg.Name)
	}

	info := initializeAnswer(t)
	if info["name"] != "srv.example.com" {
		t.Errorf("initialize must report the configured name, got %v", info["name"])
	}
	if info["title"] != "MCP Exec Server (srv.example.com)" {
		t.Errorf("the title is MCP Exec Server (%s) with that name, got %v", info["title"], info["title"])
	}
	if d, ok := info["description"].(string); !ok || !strings.Contains(d, "srv.example.com") {
		t.Errorf("the description must name the host it runs on, got %v", info["description"])
	}
	if v, ok := info["version"].(string); !ok || v == "" {
		t.Errorf("the version the build stamped must still be there, got %v", info["version"])
	}
}

func TestServerNameFallsBackToTheHostname(t *testing.T) {
	configForTest(t)
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no hostname to compare against on this machine")
	}
	t.Setenv("MCP_EXEC_NAME", "")
	path := iniFile(t, "[server]\nname = \nport = 9999\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if mcpxec.Cfg.Name != "" {
		t.Errorf("an empty name stays empty in the config, got %q", mcpxec.Cfg.Name)
	}
	if got := mcpxec.ServerName(); got != host {
		t.Errorf("an empty name means the hostname, expected %s, got %q", host, got)
	}

	info := initializeAnswer(t)
	if info["name"] != host {
		t.Errorf("initialize must report the hostname, got %v", info["name"])
	}
	if info["title"] != "MCP Exec Server ("+host+")" {
		t.Errorf("the title is MCP Exec Server (%s) with the hostname, got %v", info["title"], info["title"])
	}
	if d, ok := info["description"].(string); !ok || !strings.Contains(d, host) {
		t.Errorf("the description must name the host it runs on, got %v", info["description"])
	}
}

func TestEnvNameBeatsTheSection(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_NAME", "env-override")
	path := iniFile(t, "[server]\nname = from-the-section\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := mcpxec.ServerName(); got != "env-override" {
		t.Errorf("the env override must beat the section, got %q", got)
	}

	if info := initializeAnswer(t); info["title"] != "MCP Exec Server (env-override)" {
		t.Errorf("the title follows the name that won, got %v", info["title"])
	}
}

func TestValidDNSName(t *testing.T) {
	// the shape both the section and the env var must keep, and the shape the
	// generated fallback is built in: letters, digits and hyphens, dot-separated
	// labels, no hyphen at either end of a label, 63 characters per label, 255 in all
	label63 := strings.Repeat("a", 63)

	cases := []struct {
		value string
		ok    bool
	}{
		{"mcp-exec", true},
		{"srv01.example.com", true},
		{"Srv01.Example.COM", true}, // DNS is case-insensitive, it is taken as it comes
		{"1server", true},           // RFC 1123 admits digits at the start of a label
		{"192.168.0.1", true},       // ... and the all-numeric names it lets through
		{"a-b-c", true},
		{label63, true},
		{label63 + "." + label63, true},
		{"", false},         // empty is not a name — it means "not set"
		{"bad_name", false}, // the BSD hostname command takes the underscore, DNS does not
		{"-leading", false},
		{"trailing-", false},
		{"two..dots", false},
		{"trailing.dot.", false},
		{"space name", false},
		{"no@at", false},
		{label63 + "a", false}, // one character more than a label holds
		{label63 + "." + label63 + "." + label63 + "." + label63 + "." + label63, false}, // 259 in all
	}

	for _, c := range cases {
		if got := mcpxec.ValidDNSName(c.value); got != c.ok {
			t.Errorf("ValidDNSName(%q) = %v, the rule says %v", c.value, got, c.ok)
		}
	}
}

func TestRandomServerNameIsADNSNameAndDoesNotRepeat(t *testing.T) {
	first := mcpxec.RandomServerName()
	if !mcpxec.ValidDNSName(first) {
		t.Errorf("the generated fallback must keep the DNS shape, got %q", first)
	}
	if !strings.HasPrefix(first, "mcp-exec-") {
		t.Errorf("the generated name must stay recognisable as this service, got %q", first)
	}
	// 8 characters of crypto/rand — the chance of landing on the same one twice is
	// the chance this test is asked to fail on
	if second := mcpxec.RandomServerName(); second == first {
		t.Errorf("two generations must not answer the same name, both got %q", first)
	}
}

func TestConfigNameMustBeADNSName(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_NAME", "") // pin the env override so the assertions below hold
	path := iniFile(t, "[server]\nname = bad_name\n")

	err := mcpxec.LoadConfig(path)
	if err == nil {
		t.Fatal("an underscore is not a DNS character, the section must refuse it")
	}
	if !strings.Contains(err.Error(), "[server] name") {
		t.Errorf("the message must name the setting it refused, got %v", err)
	}
	if mcpxec.Cfg.Name != "" {
		t.Errorf("a refused value must not reach the config, got %q", mcpxec.Cfg.Name)
	}
}

func TestEnvNameMustBeADNSName(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_NAME", "trailing-")
	path := iniFile(t, "[server]\nname = from-the-section\n") // a valid value, so the env override is what fails

	err := mcpxec.LoadConfig(path)
	if err == nil {
		t.Fatal("a label cannot end on a hyphen, the env override must be refused too")
	}
	if !strings.Contains(err.Error(), "MCP_EXEC_NAME") {
		t.Errorf("the message must name the env var it refused, got %v", err)
	}
}

func TestRunStopsBeforeAnInvalidName(t *testing.T) {
	configForTest(t) // Run rebuilds the shared config; configForTest puts it back

	// LoadConfig is the first thing Run does, so this returns before the daemon, the
	// logger and any transport listening — the service does not start
	t.Setenv("MCP_EXEC_NAME", "")
	path := iniFile(t, "[server]\nname = bad_name\n")
	if err := mcpxec.Run("", path); err == nil || !strings.Contains(err.Error(), "[server] name") {
		t.Errorf("an invalid [server] name must stop the startup, got %v", err)
	}

	t.Setenv("MCP_EXEC_NAME", "bad_name")
	path = iniFile(t, "[server]\nname = \n") // the empty value means "not set", so it is not a validation error
	if err := mcpxec.Run("", path); err == nil || !strings.Contains(err.Error(), "MCP_EXEC_NAME") {
		t.Errorf("an invalid MCP_EXEC_NAME must stop the startup the same way, got %v", err)
	}

	// the positive control: a name that passes the rule loads, and reaches the answer
	t.Setenv("MCP_EXEC_NAME", "")
	path = iniFile(t, "[server]\nname = good-name.example\n")
	if err := mcpxec.LoadConfig(path); err != nil {
		t.Errorf("a name the rule accepts must load, got %v", err)
	}
	if mcpxec.ServerName() != "good-name.example" {
		t.Errorf("the accepted name must reach the answer, got %q", mcpxec.ServerName())
	}
}
