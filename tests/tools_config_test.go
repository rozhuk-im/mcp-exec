package mcpxec_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// Tests for the [tools] section — the per-tool switches. A tool that is switched
// off is not announced through tools/list, and a direct tools/call does not reach
// its handler either; a tool the section does not name keeps the default (enabled).

// configForTest pins the process-wide config for one test and restores it
// afterwards (the tests share the global Cfg), so a test may replace the Tools
// map or run LoadConfig without leaking into its neighbours.
func configForTest(t *testing.T) {
	t.Helper()
	old := mcpxec.Cfg
	t.Cleanup(func() { mcpxec.Cfg = old })
}

// iniFile writes a configuration file into the test's own directory and returns
// its path — the path the --config flag would take.
func iniFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tools.ini")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// callTool runs a tools/call round trip for a named tool through the exported
// API, the same path an MCP client takes.
func callTool(t *testing.T, toolName, args string) *mcpxec.MCPResponse {
	t.Helper()
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "` + toolName + `", "arguments": ` + args + `}`),
	}
	return mcpxec.HandleRequest(req)
}

// toolNames pulls the announced names out of a tools/list answer, in order.
func toolNames(t *testing.T, resp *mcpxec.MCPResponse) []string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("expected a tools/list result, got a JSON-RPC error: %v", resp.Error)
	}
	result, ok := resp.Result.(mcpxec.ListToolsResponse)
	if !ok {
		t.Fatalf("expected ListToolsResponse, got %T", resp.Result)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestToolsSectionParsesSwitches(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_PORT", "") // pin the env override so the assertions below hold
	path := iniFile(t, "[server]\nport = 9999\n\n[tools]\nexec = off\nshell = yes\nedit_file = true\nmystery_tool = false\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if mcpxec.Cfg.Port != 9999 {
		t.Errorf("the [tools] section must not confuse the reader, port should stay 9999, got %d", mcpxec.Cfg.Port)
	}
	if mcpxec.ToolEnabled("exec") {
		t.Error("exec = off must switch the tool off")
	}
	if !mcpxec.ToolEnabled("shell") {
		t.Error("shell = yes must switch the tool on")
	}
	if !mcpxec.ToolEnabled("edit_file") {
		t.Error("edit_file = true must keep the tool on")
	}
	if !mcpxec.ToolEnabled("unmentioned_tool") {
		t.Error("a name the section does not list keeps the default (enabled)")
	}
	if mcpxec.ToolEnabled("mystery_tool") {
		t.Error("mystery_tool = false must be recorded even for a tool that does not exist")
	}
}

func TestToolsListSkipsDisabledTool(t *testing.T) {
	configForTest(t)
	mcpxec.Cfg.Tools = map[string]bool{"shell": false}

	resp := mcpxec.HandleRequest(&mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	names := strings.Join(toolNames(t, resp), ",")
	if names != "exec,edit_file" {
		t.Errorf("a disabled tool must not be announced, expected exec and edit_file, got %q", names)
	}
}

func TestToolsListEmptyWhenAllToolsOff(t *testing.T) {
	configForTest(t)
	mcpxec.Cfg.Tools = map[string]bool{"exec": false, "shell": false, "edit_file": false}

	resp := mcpxec.HandleRequest(&mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	if len(toolNames(t, resp)) != 0 {
		t.Errorf("every switched-off tool must stay out of the list, got %v", toolNames(t, resp))
	}

	// and the empty list marshals as [], not null — that is what the client sees
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"tools":[]`) {
		t.Errorf("the empty announcement must marshal as an empty list, got %s", b)
	}
}

func TestDisabledToolCallIsRefused(t *testing.T) {
	configForTest(t)
	mcpxec.Cfg.Tools = map[string]bool{"exec": false}

	// the handler must not run: touch would have created the marker file
	marker := filepath.Join(t.TempDir(), "must_not_exist")
	// the path goes into the JSON in the slash form the rest of the suite uses — a
	// native Windows path would put raw backslashes into a JSON string, and JSON
	// would read them as escape sequences
	resp := callTool(t, "exec", `{"command":"touch","args":["`+filepath.ToSlash(marker)+`"]}`)

	if resp.Error == nil {
		t.Fatalf("a disabled tool must be refused, got result %#v", resp.Result)
	}
	if resp.Error.Code != -32602 {
		t.Errorf("a disabled tool is refused the way an unknown tool is, code -32602, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "Invalid params" {
		t.Errorf("expected the message \"Invalid params\", got %q", resp.Error.Message)
	}
	if data, ok := resp.Error.Data.(string); !ok || !strings.Contains(data, "exec") {
		t.Errorf("data must name the refused tool, got %#v", resp.Error.Data)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the handler must not run for a disabled tool")
	}
}

func TestEnvDisabledToolsOverrideTheSection(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_DISABLED_TOOLS", "shell,edit_file")
	path := iniFile(t, "[tools]\nshell = true\nexec = false\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if mcpxec.ToolEnabled("shell") {
		t.Error("the env override must beat the section: shell must be off")
	}
	if mcpxec.ToolEnabled("edit_file") {
		t.Error("a tool named only by the env override must be off")
	}
	if mcpxec.ToolEnabled("exec") {
		t.Error("a tool the section switched off stays off")
	}
}

func TestDisabledToolNeighboursStayReachable(t *testing.T) {
	configForTest(t)
	mcpxec.Cfg.Tools = map[string]bool{"exec": false}

	// the positive control: only the switched-off tool is affected
	names := strings.Join(toolNames(t, mcpxec.HandleRequest(&mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})), ",")
	if names != "shell,edit_file" {
		t.Errorf("the neighbours of a disabled tool must stay announced, got %q", names)
	}

	// the shell tool answers through its documented default, /bin/sh, which is not
	// everywhere — the announcement above is what is checked on every platform
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh is not available on this platform")
	}

	resp := callTool(t, "shell", `{"command":"echo reachable","timeout":30}`)
	if resp.Error != nil {
		t.Fatalf("the enabled neighbour must answer the usual way, got JSON-RPC error %v", resp.Error)
	}
	if text := resultText(t, resp); !strings.Contains(text, "reachable") {
		t.Errorf("the enabled neighbour must still run its command, got %q", text)
	}
}
