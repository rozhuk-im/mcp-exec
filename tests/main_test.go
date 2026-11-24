package mcpxec_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

func TestHandleInitialize(t *testing.T) {
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	}

	resp := mcpxec.HandleRequest(req)

	if resp.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC 2.0, got %s", resp.JSONRPC)
	}

	if resp.Error != nil {
		t.Errorf("Expected no error, got %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be map[string]interface{}")
	}

	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("Expected protocolVersion 2025-06-18, got %v", result["protocolVersion"])
	}
}

func TestHandleListTools(t *testing.T) {
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	}

	resp := mcpxec.HandleRequest(req)

	result, ok := resp.Result.(mcpxec.ListToolsResponse)
	if !ok {
		t.Fatal("Expected result to be ListToolsResponse")
	}

	if len(result.Tools) == 0 {
		t.Error("Expected at least one tool")
	}

	// Check for exec tool
	foundExec := false
	for _, tool := range result.Tools {
		if tool.Name == mcpxec.ToolName("exec") {
			foundExec = true
			break
		}
	}

	if !foundExec {
		t.Error("Expected exec tool")
	}
}

func TestHandleToolsCallExec(t *testing.T) {
	// Set config for testing
	oldAuth := mcpxec.Cfg.AuthKeys
	oldRespSize := mcpxec.Cfg.MaxRespSize
	oldChroot := mcpxec.Cfg.ChrootPath
	mcpxec.Cfg.AuthKeys = ""
	mcpxec.Cfg.MaxRespSize = 10485760
	mcpxec.Cfg.ChrootPath = ""
	defer func() {
		mcpxec.Cfg.AuthKeys = oldAuth
		mcpxec.Cfg.MaxRespSize = oldRespSize
		mcpxec.Cfg.ChrootPath = oldChroot
	}()

	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "` + mcpxec.ToolName("exec") + `", "arguments": {"command": "echo", "args": ["hello"]}}`),
	}

	resp := mcpxec.HandleRequest(req)

	if resp.Error != nil {
		t.Fatalf("Expected no error, got %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be map[string]interface{}")
	}

	content, ok := result["content"].([]map[string]interface{})
	if !ok {
		t.Fatal("Expected content to be []map[string]interface{}")
	}

	if len(content) == 0 {
		t.Fatal("Expected content to have at least one element")
	}

	text, ok := content[0]["text"].(string)
	if !ok {
		t.Fatal("Expected text to be string")
	}

	if text == "" {
		t.Error("Expected non-empty text")
	}
}

func TestHandleNotificationNoResponse(t *testing.T) {
	// JSON-RPC notifications must not receive a response
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}

	if resp := mcpxec.HandleRequest(req); resp != nil {
		t.Errorf("Expected nil response for notification, got %+v", resp)
	}
}

func TestHandleUnknownTool(t *testing.T) {
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "nope", "arguments": {}}`),
	}

	resp := mcpxec.HandleRequest(req)
	if resp.Error == nil {
		t.Fatal("Expected error for unknown tool")
	}
	if resp.Error.Code != -32602 {
		t.Errorf("Expected error code -32602, got %d", resp.Error.Code)
	}
}

func TestHandleResourcesAndPrompts(t *testing.T) {
	for _, method := range []string{"resources/list", "prompts/list"} {
		req := &mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: method}
		resp := mcpxec.HandleRequest(req)
		if resp.Error != nil {
			t.Errorf("%s: expected no error, got %v", method, resp.Error)
		}
	}
}

func TestDecodeRequestNumberID(t *testing.T) {
	// Large integer IDs must survive decode->marshal without float truncation
	req, err := mcpxec.DecodeRequest(strings.NewReader(`{"jsonrpc":"2.0","id":9007199254740993,"method":"ping"}`))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	id, ok := req.ID.(json.Number)
	if !ok {
		t.Fatalf("Expected id to be json.Number, got %T", req.ID)
	}
	if id.String() != "9007199254740993" {
		t.Errorf("Unexpected id %s", id.String())
	}
}

func TestResolveDir(t *testing.T) {
	oldChroot := mcpxec.Cfg.ChrootPath
	defer func() { mcpxec.Cfg.ChrootPath = oldChroot }()

	// With chroot, ".." must not escape the new root; the result is spelled the way
	// the OS spells it, which is the same string as /etc everywhere except Windows
	mcpxec.Cfg.ChrootPath = "/chroot"
	if got := mcpxec.ResolveDir("../etc"); got != filepath.Clean("/etc") {
		t.Errorf("chroot mode: expected %s, got %s", filepath.Clean("/etc"), got)
	}

	// Without chroot, the path is a host path as-is
	mcpxec.Cfg.ChrootPath = ""
	if got := mcpxec.ResolveDir("/tmp"); got != "/tmp" {
		t.Errorf("no chroot: expected /tmp, got %s", got)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	oldAuth := mcpxec.Cfg.AuthKeys
	mcpxec.Cfg.AuthKeys = ""
	defer func() { mcpxec.Cfg.AuthKeys = oldAuth }()

	rec := httptest.NewRecorder()
	mcpxec.HandleMCP(rec, httptest.NewRequest("GET", "/mcp", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rec.Code)
	}
}

func TestOptionsPreflight(t *testing.T) {
	rec := httptest.NewRecorder()
	mcpxec.HandleMCP(rec, httptest.NewRequest("OPTIONS", "/mcp", nil))

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for OPTIONS preflight, got %d", rec.Code)
	}
}

func TestRequestTooLarge(t *testing.T) {
	oldAuth := mcpxec.Cfg.AuthKeys
	oldReqSize := mcpxec.Cfg.MaxReqSize
	mcpxec.Cfg.AuthKeys = ""
	mcpxec.Cfg.MaxReqSize = 16
	defer func() {
		mcpxec.Cfg.AuthKeys = oldAuth
		mcpxec.Cfg.MaxReqSize = oldReqSize
	}()

	body := `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("x", 200) + `"}}`
	rec := httptest.NewRecorder()
	mcpxec.HandleMCP(rec, httptest.NewRequest("POST", "/mcp", strings.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected 413, got %d", rec.Code)
	}
}

func TestCheckAuth(t *testing.T) {
	oldAuth := mcpxec.Cfg.AuthKeys
	defer func() {
		mcpxec.Cfg.AuthKeys = oldAuth
	}()

	// Test with no auth keys configured
	mcpxec.Cfg.AuthKeys = ""
	req := httptest.NewRequest("POST", "/mcp", nil)

	err := mcpxec.CheckAuth(req)
	if err != nil {
		t.Errorf("Expected no error when no auth required, got %v", err)
	}

	// Test with auth key configured but not provided
	mcpxec.Cfg.AuthKeys = "test-key"
	err = mcpxec.CheckAuth(req)
	if err == nil {
		t.Error("Expected error when auth required but not provided")
	}

	// Test with correct auth key
	req = httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer test-key")

	err = mcpxec.CheckAuth(req)
	if err != nil {
		t.Errorf("Expected no error with valid auth, got %v", err)
	}

	// Test with X-API-Key header
	req = httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("X-API-Key", "test-key")

	err = mcpxec.CheckAuth(req)
	if err != nil {
		t.Errorf("Expected no error with valid X-API-Key, got %v", err)
	}

	// HTTP handler must answer with JSON-RPC error + 401, not plain text
	rec := httptest.NewRecorder()
	mcpxec.HandleMCP(rec, httptest.NewRequest("POST", "/mcp", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 from handler, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("Expected JSON-RPC error body, got %s", rec.Body.String())
	}
}

func TestFormatResult(t *testing.T) {
	result := mcpxec.ExecResult{
		Stdout:   "hello\n",
		Stderr:   "some warning\n",
		ExitCode: 0,
		Executed: "echo hello",
	}

	formatted := mcpxec.FormatResult(result)

	if formatted == "" {
		t.Error("Expected non-empty formatted result")
	}

	if !bytes.Contains([]byte(formatted), []byte("hello")) {
		t.Error("Expected formatted result to contain stdout")
	}

	if !bytes.Contains([]byte(formatted), []byte("Exit code")) {
		t.Error("Expected formatted result to contain exit code")
	}

	if !bytes.Contains([]byte(formatted), []byte("STDERR")) {
		t.Error("Expected formatted result to contain stderr section")
	}
}

func TestFormatResultStderrWhateverExitCode(t *testing.T) {
	// stderr is always part of the result: on a successful run (exit code 0),
	// on a failing one, and even when the process wrote nothing to stderr —
	// so the caller can tell "nothing on stderr" from "stderr was dropped"
	cases := []struct {
		result mcpxec.ExecResult
		want   string
	}{
		{mcpxec.ExecResult{Stdout: "out\n", Stderr: "warning\n", ExitCode: 0, Executed: "echo hello"}, "warning\n"},
		{mcpxec.ExecResult{Stdout: "", Stderr: "boom\n", ExitCode: 3, Executed: "false"}, "boom\n"},
		{mcpxec.ExecResult{Stdout: "out\n", Stderr: "", ExitCode: 0, Executed: "true"}, ""},
	}

	for _, c := range cases {
		formatted := mcpxec.FormatResult(c.result)

		if !strings.Contains(formatted, "--- STDERR ---") {
			t.Errorf("exit code %d: stderr section must always be present, got %q", c.result.ExitCode, formatted)
		}
		if c.want != "" && !strings.Contains(formatted, c.want) {
			t.Errorf("exit code %d: stderr content must be returned, got %q", c.result.ExitCode, formatted)
		}
	}
}

func TestStderrReturnedOnSuccessfulRun(t *testing.T) {
	// End to end: a tool call that exits 0 but writes to stderr must get that
	// stderr back — it is not tied to a non-zero exit code
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh is not available on this platform")
	}

	oldAuth := mcpxec.Cfg.AuthKeys
	oldChroot := mcpxec.Cfg.ChrootPath
	oldRespSize := mcpxec.Cfg.MaxRespSize
	mcpxec.Cfg.AuthKeys = ""
	mcpxec.Cfg.ChrootPath = ""
	mcpxec.Cfg.MaxRespSize = 10485760
	defer func() {
		mcpxec.Cfg.AuthKeys = oldAuth
		mcpxec.Cfg.ChrootPath = oldChroot
		mcpxec.Cfg.MaxRespSize = oldRespSize
	}()

	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "` + mcpxec.ToolName("shell") + `", "arguments": {"command": "echo out; echo oops >&2", "timeout": 30}}`),
	}

	resp := mcpxec.HandleRequest(req)
	if resp.Error != nil {
		t.Fatalf("Expected no error, got %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be map[string]interface{}")
	}
	content, ok := result["content"].([]map[string]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("Expected content to be a non-empty []map[string]interface{}, got %#v", result["content"])
	}
	text, ok := content[0]["text"].(string)
	if !ok {
		t.Fatal("Expected text to be string")
	}

	if !strings.Contains(text, "Exit code: 0") {
		t.Errorf("Expected exit code 0 in the result, got %q", text)
	}
	if !strings.Contains(text, "oops") {
		t.Errorf("stderr of a successful run must be returned, got %q", text)
	}
}

func TestParseBool(t *testing.T) {
	if !mcpxec.ParseBool("true") || !mcpxec.ParseBool("1") || !mcpxec.ParseBool("yes") || !mcpxec.ParseBool("on") {
		t.Error("Expected true for true/1/yes/on")
	}
	if mcpxec.ParseBool("false") || mcpxec.ParseBool("no") || mcpxec.ParseBool("") {
		t.Error("Expected false for false/no/empty")
	}
}

func TestRunStdioRoundtrip(t *testing.T) {
	// Verify that a JSON-RPC request line produces a valid response object
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      json.Number("42"),
		Method:  "ping",
	}

	resp := mcpxec.HandleRequest(req)
	if resp.Error != nil {
		t.Fatalf("Expected no error, got %v", resp.Error)
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decoded mcpxec.MCPResponse
	if err := json.Unmarshal(respBytes, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if decoded.JSONRPC != "2.0" {
		t.Errorf("Expected JSONRPC 2.0, got %s", decoded.JSONRPC)
	}
}

// callExecTool runs a full tools/call round trip for the exec/shell tools, pinning
// the process config to the plain defaults the other tool tests use.
func callExecTool(t *testing.T, tool, args string) *mcpxec.MCPResponse {
	t.Helper()

	old := mcpxec.Cfg
	t.Cleanup(func() { mcpxec.Cfg = old })
	mcpxec.Cfg.AuthKeys = ""
	mcpxec.Cfg.ChrootPath = ""
	mcpxec.Cfg.MaxRespSize = 10485760

	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "` + mcpxec.ToolName(tool) + `", "arguments": ` + args + `}`),
	}
	return mcpxec.HandleRequest(req)
}

// A process that never started has to reach the caller through MCP, not only the
// log: a failure that stops at the log is invisible to the client, which otherwise
// gets an empty "exit code 0" report for a run that never happened. The exec/shell
// tools report it as the JSON-RPC error they use (-32000), the process error
// travelling in data — the boundary is the process itself: a command that ran and
// exited non-zero is still a normal outcome (see the test below).
func TestExecToolStartFailureIsReturned(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
		// the program the process error must name (Windows resolves extensions,
		// so only the bare name is checked)
		want string
	}{
		{"program not in $PATH", "exec", `{"command":"mcp-exec-no-such-program"}`, "mcp-exec-no-such-program"},
		{"whole command line as the program", "exec", `{"command":"ls -la","args":["/tmp/mcp-exec"]}`, "ls -la"},
		{"path does not exist", "exec", `{"command":"mcp-exec-no-such-dir/mcp-exec-no-such-program","args":["-la"]}`, "mcp-exec-no-such-program"},
		{"shell does not exist", "shell", `{"command":"echo hello","shell":"mcp-exec-no-such-dir/mcp-exec-no-such-shell","timeout":30}`, "mcp-exec-no-such-shell"},
	}

	for _, c := range cases {
		resp := callExecTool(t, c.tool, c.args)

		if resp.Error == nil {
			t.Errorf("%s: a process that never started must come back as a JSON-RPC error, got result %#v", c.name, resp.Result)
			continue
		}
		if resp.Error.Code != -32000 {
			t.Errorf("%s: exec/shell failures use the server code -32000, got %d", c.name, resp.Error.Code)
		}
		if resp.Error.Message != "Execution error" {
			t.Errorf("%s: expected the message \"Execution error\", got %q", c.name, resp.Error.Message)
		}
		data, ok := resp.Error.Data.(string)
		if !ok || data == "" {
			t.Errorf("%s: the process error must travel in data, got %#v", c.name, resp.Error.Data)
			continue
		}
		if !strings.Contains(data, c.want) {
			t.Errorf("%s: data must name the program that could not be started (%q), got %q", c.name, c.want, data)
		}
	}
}

func TestExecToolNonZeroExitIsStillNotAnError(t *testing.T) {
	// The other side of the line: a command that ran and exited non-zero is a
	// normal outcome for an exec server and stays a result, not an error
	if _, err := exec.LookPath("/bin/sh"); err != nil {
		t.Skip("/bin/sh is not available on this platform")
	}

	resp := callExecTool(t, "shell", `{"command":"echo out; echo oops >&2; exit 7","timeout":30}`)
	if resp.Error != nil {
		t.Fatalf("a non-zero exit code is a normal outcome, got JSON-RPC error %v", resp.Error)
	}

	text := resultText(t, resp)
	if !strings.Contains(text, "Exit code: 7") {
		t.Errorf("expected exit code 7 in the report, got %q", text)
	}
	if !strings.Contains(text, "oops") {
		t.Errorf("stderr of a failing run must be returned, got %q", text)
	}
}

// Logging and answering are not alternatives: the failure must reach both. The
// test runs the logger in file mode, which is the exported path the server itself
// takes (SetupLogger), so nothing is added to the API for the sake of the test.
func TestExecToolStartFailureIsAlsoLogged(t *testing.T) {
	old := mcpxec.Cfg
	mcpxec.Cfg.AuthKeys = ""
	mcpxec.Cfg.LogMode = "file"
	mcpxec.Cfg.LogFile = filepath.Join(t.TempDir(), "mcp-exec.log")
	if err := mcpxec.SetupLogger(); err != nil {
		t.Fatalf("setup logger: %v", err)
	}
	defer func() {
		mcpxec.Cfg = old
		mcpxec.SetupLogger() // put the shared logger back
	}()

	resp := callExecTool(t, "exec", `{"command":"mcp-exec-no-such-program"}`)
	if resp.Error == nil {
		t.Fatalf("the failure must be reported to the caller, got result %#v", resp.Result)
	}

	logged, err := os.ReadFile(mcpxec.Cfg.LogFile)
	if err != nil {
		t.Fatalf("read the log file: %v", err)
	}
	if !strings.Contains(string(logged), "exec error") || !strings.Contains(string(logged), "mcp-exec-no-such-program") {
		t.Errorf("the failure must also reach the log, got %q", string(logged))
	}
	// and the caller gets the same message in the response
	if data, ok := resp.Error.Data.(string); !ok || !strings.Contains(data, "mcp-exec-no-such-program") {
		t.Errorf("the response must carry the failure too, got %#v", resp.Error.Data)
	}
}

// The same failure over the transport itself: a JSON-RPC error body with HTTP 200
// (that is how this server returns protocol errors), not the empty report the
// logger-only path used to hand the client.
func TestHandleMCPExecStartFailureOverHTTP(t *testing.T) {
	old := mcpxec.Cfg
	mcpxec.Cfg.AuthKeys = ""
	mcpxec.Cfg.MaxReqSize = 1048576
	mcpxec.Cfg.MaxRespSize = 10485760
	defer func() { mcpxec.Cfg = old }()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + mcpxec.ToolName("exec") + `","arguments":{"command":"mcp-exec-no-such-program"}}}`
	rec := httptest.NewRecorder()
	mcpxec.HandleMCP(rec, httptest.NewRequest("POST", "/mcp", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Errorf("a JSON-RPC error is returned with 200, got %d", rec.Code)
	}

	var resp mcpxec.MCPResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode the response: %v", err)
	}
	if resp.Error == nil {
		t.Fatalf("the failure must reach the caller as a JSON-RPC error, got %s", rec.Body.String())
	}
	if resp.Error.Code != -32000 || resp.Error.Message != "Execution error" {
		t.Errorf("expected the exec error -32000 \"Execution error\", got %d %q", resp.Error.Code, resp.Error.Message)
	}
	if data, ok := resp.Error.Data.(string); !ok || !strings.Contains(data, "mcp-exec-no-such-program") {
		t.Errorf("the process error must travel in data, got %#v", resp.Error.Data)
	}
}
