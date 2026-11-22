package mcpxec_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mcpxec "github.com/mcp-exec/src"
)

// Tests for the edit_file tool — the standard line-based editing tool of the MCP
// reference filesystem server: each edit replaces an exact line sequence, the
// reply is a git-style diff, dryRun previews without writing, and paths stay
// inside the configured roots. These drive the tool through the exported API, the
// same path an MCP client takes.

// testFileConfig sets the file-tool part of the process-wide config for one test
// and restores it afterwards (the tests share the global Cfg).
func testFileConfig(t *testing.T, allowedDirs []string, chroot string, maxFileSize int64) {
	t.Helper()
	old := mcpxec.Cfg
	t.Cleanup(func() { mcpxec.Cfg = old })
	mcpxec.Cfg.AllowedDirs = allowedDirs
	mcpxec.Cfg.ChrootPath = chroot
	mcpxec.Cfg.MaxFileSize = maxFileSize
}

// tempFile puts content into a file named sample.txt inside a test directory and
// returns its path.
func tempFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// callEditFile runs a full tools/call round trip for the edit_file tool.
func callEditFile(t *testing.T, args string) *mcpxec.MCPResponse {
	t.Helper()
	req := &mcpxec.MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name": "edit_file", "arguments": ` + args + `}`),
	}
	return mcpxec.HandleRequest(req)
}

// resultText pulls the text content block out of a successful tools/call result.
func resultText(t *testing.T, resp *mcpxec.MCPResponse) string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("expected a tool result, got a JSON-RPC error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected the result to be a map, got %T", resp.Result)
	}
	content, ok := result["content"].([]map[string]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("expected a non-empty content array, got %#v", resp.Result)
	}
	text, ok := content[0]["text"].(string)
	if !ok {
		t.Fatalf("expected text to be a string, got %T", content[0]["text"])
	}
	return text
}

// toolFailureText checks that a failing tool call answered the way the MCP spec
// says it must: a tool result with isError:true, not a JSON-RPC protocol error.
func toolFailureText(t *testing.T, resp *mcpxec.MCPResponse) string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("a tool failure must be a result with isError:true, got JSON-RPC error %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected the result to be a map, got %T", resp.Result)
	}
	if v, ok := result["isError"].(bool); !ok || !v {
		t.Errorf("expected isError:true, got %#v", result)
	}
	content, ok := result["content"].([]map[string]interface{})
	if !ok || len(content) == 0 {
		t.Fatalf("expected a non-empty content array, got %#v", result)
	}
	text, ok := content[0]["text"].(string)
	if !ok {
		t.Fatalf("expected text to be a string, got %T", content[0]["text"])
	}
	return text
}

func TestEditFileToolInList(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	resp := mcpxec.HandleRequest(&mcpxec.MCPRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	result, ok := resp.Result.(mcpxec.ListToolsResponse)
	if !ok {
		t.Fatalf("expected ListToolsResponse, got %T", resp.Result)
	}

	var tool *mcpxec.Tool
	for i, tl := range result.Tools {
		if tl.Name == "edit_file" {
			tool = &result.Tools[i]
		}
	}
	if tool == nil {
		t.Fatal("edit_file must be in tools/list")
	}

	// the wording of the standard tool
	for _, want := range []string{"line-based edits", "exact line sequences", "git-style diff", "allowed directories"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("the description must mention %q, got %q", want, tool.Description)
		}
	}

	schema := tool.InputSchema
	if schema.Type != "object" {
		t.Errorf("the input must be an object, got %s", schema.Type)
	}
	if len(schema.Required) != 2 || schema.Required[0] != "path" || schema.Required[1] != "edits" {
		t.Errorf("path and edits must be required, got %v", schema.Required)
	}
	for _, key := range []string{"path", "edits", "dryRun"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("the schema must describe %s, got %v", key, schema.Properties)
		}
	}
	dryRun, ok := schema.Properties["dryRun"].(map[string]any)
	if !ok {
		t.Fatal("dryRun must be a schema object")
	}
	if dryRun["type"] != "boolean" {
		t.Errorf("dryRun must be a boolean, got %v", dryRun["type"])
	}
	// line-based edits: lineStart/lineEnd/text are required for every edit
	edits, ok := schema.Properties["edits"].(map[string]any)
	if !ok {
		t.Fatal("edits must be a schema object")
	}
	if edits["type"] != "array" {
		t.Errorf("edits must be an array, got %v", edits["type"])
	}
	items, ok := edits["items"].(map[string]any)
	if !ok {
		t.Fatal("the array must describe its items")
	}
	required, ok := items["required"].([]string)
	if !ok || len(required) != 3 {
		t.Fatalf("every edit must require lineStart, lineEnd and text, got %#v", items["required"])
	}
	for _, key := range []string{"lineStart", "lineEnd", "text"} {
		found := false
		for _, r := range required {
			if r == key {
				found = true
			}
		}
		if !found {
			t.Errorf("the item schema must require %s, got %v", key, required)
		}
	}
	// protocol 2025-06-18 annotations: the tool writes, so the hints must be visible
	if tool.Annotations == nil {
		t.Fatal("edit_file must carry the annotation hints the protocol defines")
	}
	if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
		t.Error("a tool that writes a file must carry the destructive hint")
	}
	if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
		t.Error("a tool confined to the local filesystem must not claim the open-world hint")
	}
}

func TestEditFileAppliesEditsAndReturnsGitDiff(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	path := filepath.ToSlash(tempFile(t, t.TempDir(), "one\ntwo\nthree\nfour\n"))

	resp := callEditFile(t, `{"path": "`+path+`", "edits": [{"lineStart": 2, "lineEnd": 3, "text": "replaced"}]}`)
	text := resultText(t, resp)

	lines := strings.Split(text, "\n")
	if len(lines) < 5 {
		t.Fatalf("the standard reply is the git-style diff of the change, got %q", text)
	}
	if lines[0] != "Index: "+path { // unconfined — the header shows the resolved path
		t.Errorf("unexpected Index header %q", lines[0])
	}
	if lines[1] != strings.Repeat("=", 71) {
		t.Errorf("the git header separator must be 71 equal signs, got %q", lines[1])
	}
	if lines[2] != "--- "+path || lines[3] != "+++ "+path {
		t.Errorf("---/+++ headers expected, got %q and %q", lines[2], lines[3])
	}
	if !strings.HasPrefix(lines[4], "@@ -1,4 +1,3 @@") {
		t.Errorf("unexpected hunk header %q", lines[4])
	}
	if !strings.Contains(text, "-two\n") || !strings.Contains(text, "-three\n") || !strings.Contains(text, "+replaced\n") {
		t.Errorf("the diff must show the change, got %q", text)
	}

	// and the file itself must have changed
	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	want := "one\nreplaced\nfour\n"
	if string(data) != want {
		t.Errorf("expected %q on disk, got %q", want, string(data))
	}
}

func TestEditFileDryRunPreviewsWithoutWriting(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	path := filepath.ToSlash(tempFile(t, t.TempDir(), "a\nb\nc\n"))

	resp := callEditFile(t, `{"path": "`+path+`", "edits": [{"lineStart": 2, "lineEnd": 2, "text": "x"}], "dryRun": true}`)
	text := resultText(t, resp)

	if !strings.Contains(text, "-b\n") || !strings.Contains(text, "+x\n") {
		t.Errorf("the preview must show the change, got %q", text)
	}
	if !strings.Contains(text, "@@ -") {
		t.Errorf("the preview must be a git-style diff, got %q", text)
	}

	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nb\nc\n" {
		t.Errorf("dryRun must not write the file, got %q", string(data))
	}
}

func TestExecuteEditFileAppliesEditsBottomUp(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	path := filepath.ToSlash(tempFile(t, t.TempDir(), "1\n2\n3\n4\n5\n6\n"))

	// the bottom edit is listed first: line numbers are checked against the
	// original file, so both ranges must land where the caller meant them
	args := `{"path": "` + path + `", "edits": [{"lineStart": 5, "lineEnd": 6, "text": "five"}, {"lineStart": 1, "lineEnd": 2, "text": "one"}]}`

	res, err := mcpxec.ExecuteEditFile(json.RawMessage(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, ok := res.(*mcpxec.EditResult)
	if !ok {
		t.Fatalf("expected *mcpxec.EditResult, got %T", res)
	}
	if !result.Changed {
		t.Error("the edits changed the file, Changed must be true")
	}
	if result.DryRun {
		t.Error("dryRun was not requested")
	}

	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	want := "one\n3\n4\nfive\n"
	if string(data) != want {
		t.Errorf("expected %q on disk, got %q", want, string(data))
	}
	if !strings.Contains(result.Diff, "+one\n") || !strings.Contains(result.Diff, "+five\n") {
		t.Errorf("the diff must cover both edits, got %q", result.Diff)
	}
}

func TestApplyEditsBottomUp(t *testing.T) {
	before := "a\nb\nc\nd\n"
	edits := []mcpxec.LineEdit{
		{LineStart: json.Number("4"), LineEnd: json.Number("4"), Text: "four", Context: &mcpxec.LineContext{Line: json.Number("1"), Text: "a"}},
		{LineStart: json.Number("2"), LineEnd: json.Number("3"), Text: ""}, // an empty text deletes the range
	}

	got := mcpxec.ApplyEdits(before, edits)
	want := "a\nfour\n"
	if got != want {
		t.Errorf("edits applied bottom-up must give %q, got %q", want, got)
	}
}

func TestGenerateGitDiffFormat(t *testing.T) {
	before := "one\ntwo\nthree\n"
	after := "one\nTWO\nthree\n"

	diff := mcpxec.GenerateGitDiff("a.txt", before, after)
	lines := strings.Split(diff, "\n")
	if len(lines) < 6 {
		t.Fatalf("the diff is too short: %q", diff)
	}
	if lines[0] != "Index: a.txt" {
		t.Errorf("unexpected Index header %q", lines[0])
	}
	if lines[1] != strings.Repeat("=", 71) {
		t.Errorf("the git header separator must be 71 equal signs, got %q", lines[1])
	}
	if lines[2] != "--- a.txt" || lines[3] != "+++ a.txt" {
		t.Errorf("---/+++ headers expected, got %q and %q", lines[2], lines[3])
	}
	if lines[4] != "@@ -1,3 +1,3 @@" {
		t.Errorf("unexpected hunk header %q", lines[4])
	}
	if !strings.Contains(diff, " one\n-two\n+TWO\n three\n") {
		t.Errorf("the hunk must show context around the change, got %q", diff)
	}

	if mcpxec.GenerateGitDiff("a.txt", before, before) != "" {
		t.Error("identical content must produce no diff")
	}
}

func TestGenerateGitDiffTrailingNewline(t *testing.T) {
	// the file has no trailing newline; git says so with the standard marker
	before := "a\nb"
	after := "a\nnew\nb"

	diff := mcpxec.GenerateGitDiff("a.txt", before, after)
	if !strings.Contains(diff, "@@ -1,2 +1,3 @@") {
		t.Errorf("unexpected hunk header, got %q", diff)
	}
	if !strings.Contains(diff, "+new\n") {
		t.Errorf("the addition is missing, got %q", diff)
	}
	if !strings.Contains(diff, "\\ No newline at end of file") {
		t.Errorf("git must mark the missing trailing newline, got %q", diff)
	}
}

func TestResolveEditPathConfinement(t *testing.T) {
	old := mcpxec.Cfg
	defer func() { mcpxec.Cfg = old }()

	root := t.TempDir()
	outside := t.TempDir()
	mcpxec.Cfg.AllowedDirs = []string{root}

	inside := filepath.Join(root, "inside.txt")
	if got, err := mcpxec.ResolveEditPath(inside); err != nil || got != filepath.Clean(inside) {
		t.Errorf("a path inside the allowed root must be accepted, got %q, %v", got, err)
	}
	if _, err := mcpxec.ResolveEditPath(filepath.Join(outside, "outside.txt")); err == nil {
		t.Error("a path outside the allowed roots must be refused")
	}
	if _, err := mcpxec.ResolveEditPath("inside.txt"); err == nil {
		t.Error("relative paths must be refused when allowed_dirs is set")
	}

	// the chroot doubles as the root when allowed_dirs is empty
	mcpxec.Cfg.AllowedDirs = nil
	mcpxec.Cfg.ChrootPath = root
	if _, err := mcpxec.ResolveEditPath(inside); err != nil {
		t.Errorf("a file inside the chroot directory must be accepted, got %v", err)
	}
	if _, err := mcpxec.ResolveEditPath(filepath.Join(outside, "outside.txt")); err == nil {
		t.Error("a file outside the chroot directory must be refused")
	}

	// a symlink must not be a way out of the root
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are not available here (%v)", err)
	}
	if _, err := mcpxec.ResolveEditPath(link); err == nil {
		t.Error("a symlink pointing outside the allowed root must be refused")
	}

	// with no roots at all the path is unconfined, relative paths included
	mcpxec.Cfg.ChrootPath = ""
	if got, err := mcpxec.ResolveEditPath("relative.txt"); err != nil || !filepath.IsAbs(got) {
		t.Errorf("without roots the path stays unconfined but absolute, got %q, %v", got, err)
	}
}

func TestEditFileStaysInsideAllowedDirs(t *testing.T) {
	inside := t.TempDir()
	outside := t.TempDir()
	testFileConfig(t, []string{inside}, "", 0)

	path := filepath.ToSlash(tempFile(t, inside, "a\nb\n"))
	resp := callEditFile(t, `{"path": "`+path+`", "edits": [{"lineStart": 2, "lineEnd": 2, "text": "B"}], "dryRun": true}`)
	text := resultText(t, resp)
	// inside a root the diff header is relative to it, as the reference server renders it
	if !strings.HasPrefix(text, "Index: sample.txt\n") {
		t.Errorf("the header must be relative to the allowed root, got %q", text)
	}
	if !strings.Contains(text, "-b\n") || !strings.Contains(text, "+B\n") {
		t.Errorf("the preview must show the change, got %q", text)
	}

	other := filepath.ToSlash(tempFile(t, outside, "a\nb\n"))
	resp = callEditFile(t, `{"path": "`+other+`", "edits": [{"lineStart": 2, "lineEnd": 2, "text": "B"}], "dryRun": true}`)
	if resp.Error == nil {
		t.Fatalf("a path outside the allowed directories must be refused, got result %#v", resp.Result)
	}
	if resp.Error.Code != -32602 {
		t.Errorf("expected -32602 Invalid params, got %d (%s)", resp.Error.Code, resp.Error.Message)
	}
	if !strings.Contains(resp.Error.Message, "allowed") {
		t.Errorf("the message must explain the confinement, got %q", resp.Error.Message)
	}
}

func TestEditFileInvalidParams(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	path := filepath.ToSlash(tempFile(t, t.TempDir(), "alpha\nbeta\ngamma\n"))

	cases := []struct {
		args string
		why  string
	}{
		{`{"path": "` + path + `", "edits": [{"lineStart": 1, "lineEnd": 4, "text": "x"}]}`, "lineEnd past the end of the file"},
		{`{"path": "` + path + `", "edits": [{"lineStart": 0, "lineEnd": 1, "text": "x"}]}`, "lineStart below 1"},
		{`{"path": "` + path + `", "edits": [{"lineStart": 2, "lineEnd": 1, "text": "x"}]}`, "lineEnd before lineStart"},
		{`{"path": "` + path + `", "edits": [{"lineStart": 1.5, "lineEnd": 2, "text": "x"}]}`, "fractional line number"},
		{`{"path": "` + path + `", "edits": [{"lineStart": 1, "lineEnd": 1, "text": "x", "context": {"line": 2, "text": "not beta"}}]}`, "the context anchor does not match"},
		{`{"path": "` + path + `", "edits": []}`, "edits must contain at least one edit"},
		{`{"path": "", "edits": [{"lineStart": 1, "lineEnd": 1, "text": "x"}]}`, "path is required"},
		{`{"path": "` + path + `", "edits": [{"lineStart": 1, "lineEnd": 2, "text": "x"}, {"lineStart": 2, "lineEnd": 3, "text": "y"}]}`, "overlapping edits"},
	}

	for _, c := range cases {
		resp := callEditFile(t, c.args)
		if resp.Error == nil {
			t.Errorf("%s: expected a JSON-RPC error, got result %#v", c.why, resp.Result)
			continue
		}
		if resp.Error.Code != -32602 {
			t.Errorf("%s: expected -32602 Invalid params, got %d (%s)", c.why, resp.Error.Code, resp.Error.Message)
		}
	}

	// nothing was written — validation happens before the file is touched
	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "alpha\nbeta\ngamma\n" {
		t.Errorf("rejected calls must not touch the file, got %q", string(data))
	}
}

func TestEditFileMissingFileIsToolError(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	// per the MCP spec a tool that cannot do its job answers with a tool result
	// carrying isError:true, not with a JSON-RPC protocol error
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "missing.txt"))
	resp := callEditFile(t, `{"path": "`+path+`", "edits": [{"lineStart": 1, "lineEnd": 1, "text": "x"}], "dryRun": true}`)

	text := toolFailureText(t, resp)
	if !strings.Contains(text, "missing.txt") {
		t.Errorf("the failure must name the file, got %q", text)
	}
}

func TestEditFileNoModification(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	path := filepath.ToSlash(tempFile(t, t.TempDir(), "x\ny\n"))

	resp := callEditFile(t, `{"path": "`+path+`", "edits": [{"lineStart": 1, "lineEnd": 1, "text": "x"}]}`)
	if resp.Error != nil {
		t.Fatalf("a no-op edit is not an error, got %v", resp.Error)
	}
	if text := resultText(t, resp); text != "No modification" {
		t.Errorf("expected the standard 'No modification' answer, got %q", text)
	}

	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "x\ny\n" {
		t.Errorf("a no-op must leave the file alone, got %q", string(data))
	}
}

func TestEditFilePreservesLineEndings(t *testing.T) {
	testFileConfig(t, nil, "", 0)

	path := filepath.ToSlash(tempFile(t, t.TempDir(), "first\r\nsecond\r\nthird\r\n"))

	resp := callEditFile(t, `{"path": "`+path+`", "edits": [{"lineStart": 2, "lineEnd": 2, "text": "new"}]}`)
	text := resultText(t, resp)
	if !strings.Contains(text, "+new") {
		t.Errorf("the change must show up in the diff, got %q", text)
	}

	data, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	want := "first\r\nnew\r\nthird\r\n"
	if string(data) != want {
		t.Errorf("a CRLF file must stay CRLF, expected %q, got %q", want, string(data))
	}
}

func TestEditFileKeepsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not a Windows concept here")
	}
	testFileConfig(t, nil, "", 0)

	path := filepath.Join(t.TempDir(), "mode.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0640); err != nil {
		t.Fatal(err)
	}

	resp := callEditFile(t, `{"path": "`+filepath.ToSlash(path)+`", "edits": [{"lineStart": 2, "lineEnd": 2, "text": "B"}]}`)
	resultText(t, resp)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0640 {
		t.Errorf("the atomic rewrite must keep the original permissions, got %v", fi.Mode().Perm())
	}
}

func TestEditFileSizeLimit(t *testing.T) {
	if mcpxec.DefaultMaxFileSize != 10*1024*1024 {
		t.Errorf("the documented default is 10MB, got %d", mcpxec.DefaultMaxFileSize)
	}
	testFileConfig(t, nil, "", 10) // 10 bytes

	path := filepath.ToSlash(tempFile(t, t.TempDir(), "this file is definitely longer than ten bytes\n"))

	resp := callEditFile(t, `{"path": "`+path+`", "edits": [{"lineStart": 1, "lineEnd": 1, "text": "x"}], "dryRun": true}`)
	text := toolFailureText(t, resp)
	if !strings.Contains(text, "max_file_size") {
		t.Errorf("the size limit must be reported to the caller, got %q", text)
	}
}
