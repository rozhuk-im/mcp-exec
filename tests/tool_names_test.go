package mcpxec_test

import (
	"encoding/json"
	"strings"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// Tests for the names the tools are announced and called by: the server name, a
// hyphen, then the tool, the dots of the server name written as underscores — the
// alphabet a tool name is kept inside has no dot — and the whole name kept within 64
// characters, which a server name too long for that pays for out of its own end. Each
// tool also carries the title the protocol added: the plain tool name, which is also
// what the operator writes in the [tools] section, so one tool has two spellings and
// the registry is the place where both are resolved. The server name in these tests
// comes from a config file, so nothing here depends on the machine the suite runs on.

const testServerName = "srv.example.com"

// configWithAName loads the name from a file and pins it for the assertions below.
func configWithAName(t *testing.T) {
	t.Helper()
	configForTest(t)
	t.Setenv("MCP_EXEC_NAME", "") // pin the env override so the assertions below hold
	path := iniFile(t, "[server]\nname = "+testServerName+"\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
}

func TestToolNamesCarryTheServerPrefix(t *testing.T) {
	configWithAName(t)

	resp := mcpxec.HandleRequest(&mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	result, ok := resp.Result.(mcpxec.ListToolsResponse)
	if !ok {
		t.Fatalf("expected ListToolsResponse, got %T", resp.Result)
	}

	want := []string{"srv_example_com-exec", "srv_example_com-shell", "srv_example_com-edit_file"}
	keys := []string{"exec", "shell", "edit_file"} // the plain name: the [tools] key, and the title
	if len(result.Tools) != len(want) {
		t.Fatalf("all three tools must be announced, got %v", result.Tools)
	}

	// the definitions are built per request now, so this is also the check that they
	// come through whole — name, title, schema and hints
	for i, tool := range result.Tools {
		if tool.Name != want[i] {
			t.Errorf("tool %d must be announced as %s, got %q", i, want[i], tool.Name)
		}
		if strings.Contains(tool.Name, ".") {
			t.Errorf("an announced name carries no dot — the server name writes its dots as underscores, got %q", tool.Name)
		}
		if tool.Title != keys[i] {
			t.Errorf("tool %d must report %s as its title, got %q", i, keys[i], tool.Title)
		}
		if len(tool.InputSchema.Properties) == 0 {
			t.Errorf("%s must still carry its input schema", want[i])
		}
		if tool.Annotations == nil {
			t.Errorf("%s must still carry its annotation hints", want[i])
		}
		// the description names the server it runs on — by the plain DNS name, dots and
		// all; the announced name is where the dots are written as underscores, and the
		// bracketed spelling lives in the initialize title, not in a tool description
		if !strings.Contains(tool.Description, testServerName) {
			t.Errorf("the description must name the server it belongs to, got %q", tool.Description)
		}
	}

	// and the prefix must not have eaten the tool it is a prefix of: what a client
	// takes from tools/list and puts back into tools/call is the same string
	if mcpxec.ToolName("edit_file") != "srv_example_com-edit_file" {
		t.Errorf("ToolName must build the announced name, got %q", mcpxec.ToolName("edit_file"))
	}
}

func TestToolCallUsesTheAnnouncedName(t *testing.T) {
	configWithAName(t)

	// the path a client takes: the name from tools/list, put back into tools/call
	resp := callTool(t, "exec", `{"command":"echo","args":["via the announced name"]}`)
	if resp.Error != nil {
		t.Fatalf("the announced name must reach the handler, got JSON-RPC error %v", resp.Error)
	}
	if !strings.Contains(resultText(t, resp), "via the announced name") {
		t.Errorf("the handler must run for the announced name, got %q", resultText(t, resp))
	}
}

func TestBareToolNameIsNotANameOfThisServer(t *testing.T) {
	configWithAName(t)

	// the prefix is part of the name, so the plain tool name — what the [tools]
	// section is keyed on — is not a name a call can use, and is refused the way any
	// unknown tool is
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "exec", "arguments": {"command": "echo"}}`),
	}

	resp := mcpxec.HandleRequest(req)
	if resp.Error == nil {
		t.Fatalf("the bare tool name must not dispatch, got result %#v", resp.Result)
	}
	if resp.Error.Code != -32602 {
		t.Errorf("an unknown tool name is refused with code -32602, got %d", resp.Error.Code)
	}
	if data, ok := resp.Error.Data.(string); !ok || !strings.Contains(data, "Unknown tool: exec") {
		t.Errorf("data must name what was asked for, got %#v", resp.Error.Data)
	}
}

func TestDisabledToolIsRefusedByTheAnnouncedName(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_NAME", "") // pin the env override so the assertions below hold
	// the switch is written as the plain tool key, and must still reach the tool when
	// the call comes in with the prefix — a switch checked against the announced name
	// would default to enabled and the tool would run
	path := iniFile(t, "[server]\nname = "+testServerName+"\n\n[tools]\nexec = off\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}
	if mcpxec.ToolEnabled("exec") {
		t.Fatal("the section must have switched exec off")
	}

	resp := callTool(t, "exec", `{"command":"echo","args":["must not run"]}`)
	if resp.Error == nil {
		t.Fatalf("a disabled tool must not reach its handler, got result %#v", resp.Result)
	}
	if resp.Error.Code != -32602 || resp.Error.Message != "Invalid params" {
		t.Errorf("a disabled tool is refused the way an unknown tool is, got %v", resp.Error)
	}
	if data, ok := resp.Error.Data.(string); !ok || !strings.Contains(data, "Tool disabled") {
		t.Errorf("data must say the tool was disabled, got %#v", resp.Error.Data)
	}
}

func TestToolTitlesReachTheClientInJSON(t *testing.T) {
	configWithAName(t)

	// the way a client reads tools/list: the answer is marshaled and read back, rather
	// than reached into as Go built it. The title is the field a client shows a human,
	// so it has to survive the round trip — the plain tool name, not the announced one.
	b, err := json.Marshal(mcpxec.HandleRequest(&mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded struct {
		Result struct {
			Tools []struct {
				Name  string `json:"name"`
				Title string `json:"title"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]string{ // what a client reads in name -> what it must read in title
		"srv_example_com-exec":      "exec",
		"srv_example_com-shell":     "shell",
		"srv_example_com-edit_file": "edit_file",
	}
	if len(decoded.Result.Tools) != len(want) {
		t.Fatalf("all three tools must come through, got %v", decoded.Result.Tools)
	}
	for _, tool := range decoded.Result.Tools {
		title, ok := want[tool.Name]
		if !ok {
			t.Errorf("tools/list announced an unexpected name %q", tool.Name)
			continue
		}
		if tool.Title != title {
			t.Errorf("the tool announced as %s must carry %q as its title, got %q", tool.Name, title, tool.Title)
		}
	}
}

func TestLongServerNameLosesItsEnd(t *testing.T) {
	configForTest(t)
	t.Setenv("MCP_EXEC_NAME", "") // pin the env override so the assertions below hold

	// two labels as long as DNS allows, so the server name on its own is longer than
	// an announced name is allowed to be and has to give up its end
	label63 := strings.Repeat("a", 63)
	path := iniFile(t, "[server]\nname = "+label63+"."+label63+"\n")

	if err := mcpxec.LoadConfig(path); err != nil {
		t.Fatalf("load config: %v", err)
	}

	// 64 characters for the whole name: the separator and the tool are paid for first,
	// so what is left for the server name is 59 in front of exec and 54 in front of
	// edit_file — the tool is what the client is asking for, it always survives the
	// trim whole, and the trimmed name is still the name that dispatches
	if got, want := mcpxec.ToolName("exec"), strings.Repeat("a", 59)+"-exec"; got != want {
		t.Errorf("a name over the limit must lose the end of its server name, expected %s, got %q", want, got)
	}
	if got, want := mcpxec.ToolName("edit_file"), strings.Repeat("a", 54)+"-edit_file"; got != want {
		t.Errorf("a longer tool name leaves the server name shorter, expected %s, got %q", want, got)
	}

	resp := callTool(t, "exec", `{"command":"echo","args":["through the trimmed name"]}`)
	if resp.Error != nil {
		t.Fatalf("the trimmed name must still reach the handler, got JSON-RPC error %v", resp.Error)
	}
	if !strings.Contains(resultText(t, resp), "through the trimmed name") {
		t.Errorf("the handler must run for the trimmed name, got %q", resultText(t, resp))
	}
}
