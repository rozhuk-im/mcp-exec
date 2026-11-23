package mcpxec

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// This file implements the edit_file tool — the standard line-based file-editing
// tool of the MCP reference filesystem server: every edit replaces an exact line
// sequence with new content, the reply is a git-style diff, and dryRun previews
// the change without touching the file. Paths are confined to the configured
// roots (allowed_dirs, falling back to chroot_path); with no roots the file tool
// runs unconfined, which is the documented "trusted caller" model this server
// already uses for exec/shell. Pure stdlib, like the rest of the project.

// LineEdit is one line-based edit: the exact line sequence
// [LineStart, LineEnd] (1-indexed, inclusive) is replaced with Text.
// An empty Text deletes the range; Text may itself be several lines long.
type LineEdit struct {
	LineStart json.Number  `json:"lineStart"`
	LineEnd   json.Number  `json:"lineEnd"`
	Text      string       `json:"text"`
	Context   *LineContext `json:"context,omitempty"`
}

// LineContext anchors an edit to the original file: if it is given, line
// Context.Line must hold exactly Context.Text when the file is read. It makes an
// edit aimed at the wrong part of a file that changed under the caller fail
// loudly instead of quietly rewriting the wrong lines.
type LineContext struct {
	Line json.Number `json:"line"`
	Text string      `json:"text"`
}

// EditParams are the arguments of the edit_file tool — the standard
// path/edits/dryRun set.
type EditParams struct {
	Path   string     `json:"path"`
	Edits  []LineEdit `json:"edits"`
	DryRun bool       `json:"dryRun"`
}

// EditResult is what edit_file produces: the git-style diff of the change, which
// is exactly the text block the standard tool returns ("No modification" when
// the edits turned out to be a no-op).
type EditResult struct {
	Diff    string `json:"diff"`
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	DryRun  bool   `json:"dry_run"`
}

// invalidParamsError marks an error that has to be reported as JSON-RPC -32602:
// bad input — missing or malformed parameters, a path outside the allowed roots.
type invalidParamsError struct{ message string }

func (e *invalidParamsError) Error() string { return e.message }

// fsError marks a tool-level failure (the file is missing, a read or a write
// failed). Per the MCP spec these come back as a tool result with isError:true,
// not as a protocol error.
type fsError struct{ message string }

func (e *fsError) Error() string { return e.message }

// toolHints spells out the protocol-2025-06-18 annotation hints for a tool.
// All four are given explicitly so a client sees the intent instead of the spec
// defaults (readOnly false, destructive true, idempotent false, openWorld true).
func toolHints(readOnly, destructive, idempotent, openWorld bool) *ToolAnnotations {
	return &ToolAnnotations{
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

// editFileTool is the tool definition, worded the way the standard tool is worded.
func editFileTool() Tool {
	return Tool{
		Name:        "edit_file",
		Description: "Make line-based edits to a text file. Each edit replaces exact line sequences with new content. Returns a git-style diff showing the changes made. Only works within allowed directories.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path of the file to edit — absolute when allowed_dirs is set",
				},
				"edits": map[string]any{
					"type":        "array",
					"description": "Line-based edits, applied from the bottom of the file upwards so the line numbers stay valid",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"lineStart": map[string]any{"type": "number", "description": "First line of the edit (1-indexed)"},
							"lineEnd":   map[string]any{"type": "number", "description": "Last line of the edit (1-indexed, inclusive)"},
							"text":      map[string]any{"type": "string", "description": "Content to replace the edited lines with; may be multi-line, empty deletes the range"},
							"context": map[string]any{
								"description": "Optional anchor: this line of the original file must hold this text before the edit is applied",
								"anyOf": []any{
									map[string]any{
										"type": "object",
										"properties": map[string]any{
											"line": map[string]any{"type": "number", "description": "Line number of the anchor line (1-indexed)"},
											"text": map[string]any{"type": "string", "description": "Content the anchor line must have"},
										},
										"required": []string{"line", "text"},
									},
									map[string]any{"type": "null"},
								},
							},
						},
						"required": []string{"lineStart", "lineEnd", "text"},
					},
				},
				"dryRun": map[string]any{
					"type":        "boolean",
					"description": "Preview changes using git-style diff format",
					"default":     false,
				},
			},
			Required: []string{"path", "edits"},
		},
		Annotations: toolHints(false, true, false, false),
	}
}

// ExecuteEditFile applies the line-based edits of a standard edit_file call and
// returns the git-style diff of the change. Input problems come back as
// *invalidParamsError (JSON-RPC -32602); failures to touch the file come back as
// *fsError, which the caller renders as a tool result with isError:true.
func ExecuteEditFile(params json.RawMessage) (interface{}, error) {
	var p EditParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &invalidParamsError{message: "invalid parameters: " + err.Error()}
	}
	if strings.TrimSpace(p.Path) == "" {
		return nil, &invalidParamsError{message: "path is required"}
	}
	if len(p.Edits) == 0 {
		return nil, &invalidParamsError{message: "edits is required and must contain at least one edit"}
	}

	resolved, err := ResolveEditPath(p.Path)
	if err != nil {
		return nil, &invalidParamsError{message: err.Error()}
	}

	content, err := readTextFile(resolved)
	if err != nil {
		return nil, &fsError{message: err.Error()}
	}

	if err := validateEdits(content, p.Edits); err != nil {
		return nil, &invalidParamsError{message: err.Error()}
	}

	newContent := ApplyEdits(content, p.Edits)
	display := displayPath(resolved)

	if newContent == content {
		return &EditResult{Diff: "No modification", Path: display, Changed: false, DryRun: p.DryRun}, nil
	}

	diff := GenerateGitDiff(display, content, newContent)

	if !p.DryRun {
		// Re-read immediately before writing: the edits were validated against the
		// content read above, and a concurrent writer must not be clobbered silently
		now, err := readTextFile(resolved)
		if err != nil {
			return nil, &fsError{message: err.Error()}
		}
		if now != content {
			return nil, &fsError{message: fmt.Sprintf("%s changed while editing; re-read it and apply the edits again", resolved)}
		}
		if err := writeTextFileAtomic(resolved, newContent); err != nil {
			return nil, &fsError{message: err.Error()}
		}
	}

	return &EditResult{Diff: diff, Path: display, Changed: true, DryRun: p.DryRun}, nil
}

// splitAllowedDirs splits the comma-separated list of allowed roots (the same
// convention this server uses for auth_keys).
func splitAllowedDirs(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fileRoots returns the roots the file tool is confined to. Explicit allowed_dirs
// wins; a configured chroot doubles as the root when nothing else is set — chroot()
// is applied to the child processes only, so the server itself sees the host
// filesystem, and pointing the file tool at the same directory gives the isolation
// the chroot option promises. No roots = no confinement, matching the documented
// "trusted caller" model of this server.
func fileRoots() []string {
	roots := make([]string, 0, len(Cfg.AllowedDirs)+1)
	for _, d := range Cfg.AllowedDirs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		if abs, err := filepath.Abs(strings.TrimSpace(d)); err == nil {
			roots = append(roots, filepath.Clean(abs))
		}
	}
	if len(roots) == 0 && Cfg.ChrootPath != "" {
		if abs, err := filepath.Abs(Cfg.ChrootPath); err == nil {
			roots = append(roots, filepath.Clean(abs))
		}
	}
	return roots
}

// maxFileSizeLimit is the configured cap with the built-in default applied.
func maxFileSizeLimit() int64 {
	if Cfg.MaxFileSize > 0 {
		return Cfg.MaxFileSize
	}
	return DefaultMaxFileSize
}

// ResolveEditPath resolves the path argument of edit_file against the allowed
// roots. Confinement is checked after resolving symlinks as deep as the existing
// part of the path goes, the roots the same way, so a symlink cannot be used to
// edit something outside the roots and a root reached through a symlink keeps
// covering its own tree. With no roots configured the path is cleaned and used
// as-is.
func ResolveEditPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("path is required")
	}
	roots := fileRoots()

	abs := p
	if !filepath.IsAbs(abs) {
		if len(roots) == 0 {
			// unconfined — relative paths are taken against the server's working directory
			if wd, err := os.Getwd(); err == nil {
				abs = filepath.Join(wd, abs)
			}
		} else {
			return "", fmt.Errorf("path %s must be absolute when allowed_dirs is set", p)
		}
	}

	clean := filepath.Clean(abs)
	if len(roots) == 0 {
		return clean, nil
	}

	real, err := realpathNearest(clean)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		// the root is resolved too, so both sides of the check meet on the real
		// filesystem: a root reached through a symlink is the directory it points
		// to (macOS mounts /var — and every temporary directory under it — as
		// private/var), and comparing that root as spelled would leave everything
		// inside it outside
		if realRoot, err := realpathNearest(root); err == nil && insideRoot(realRoot, real) {
			return clean, nil
		}
		// a root that does not exist cannot be resolved, and is taken as spelled
		if insideRoot(root, real) {
			return clean, nil
		}
	}
	return "", fmt.Errorf("path %s is outside the allowed directories (%s)", p, strings.Join(roots, ", "))
}

// realpathNearest resolves symlinks as far as the path exists on disk (the file
// itself may not exist yet, so EvalSymlinks alone is not enough); the unresolved
// tail is appended back to the resolved prefix.
func realpathNearest(p string) (string, error) {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(real), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("cannot resolve symlinks on %s (%v), so it cannot be confirmed to be inside the allowed directories", p, err)
	}

	var tail []string
	path := filepath.Clean(p)
	for {
		tail = append(tail, filepath.Base(path))
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Clean(p), nil // nothing on the path exists — nothing to resolve
		}
		path = parent
		if real, err := filepath.EvalSymlinks(path); err == nil {
			out := real
			for i := len(tail) - 1; i >= 0; i-- {
				out = filepath.Join(out, tail[i])
			}
			return filepath.Clean(out), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("cannot resolve symlinks on %s (%v), so it cannot be confirmed to be inside the allowed directories", p, err)
		}
	}
}

// insideRoot reports whether a cleaned path sits inside a cleaned root
// (the path being the root itself counts as inside).
func insideRoot(root, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(p))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// displayPath is the path the diff header shows — relative to the allowed root
// when there is one (as the reference server renders it, so the absolute layout
// of the server host is not leaked into the reply), otherwise the path as resolved.
func displayPath(abs string) string {
	for _, root := range fileRoots() {
		if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return abs
}

// readTextFile reads a file for the file tool, refusing anything that is not a
// regular file and anything larger than the configured max_file_size.
func readTextFile(p string) (string, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", p, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", p)
	}
	if limit := maxFileSizeLimit(); fi.Size() > limit {
		return "", fmt.Errorf("%s is %d bytes, over max_file_size (%d bytes)", p, fi.Size(), limit)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", p, err)
	}
	return string(data), nil
}

// writeTextFileAtomic writes through a temporary file in the target's own
// directory and renames it over the target, so a reader never sees a half-written
// file (rename is atomic within one filesystem — which is exactly why the
// temporary file is put next to the target). The original permissions are kept.
func writeTextFileAtomic(p, content string) error {
	mode := os.FileMode(0644)
	if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(p), ".mcp-exec-")
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", p, err)
	}
	name := tmp.Name()
	fail := func(action string, err error) error {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("cannot %s %s: %w", action, p, err)
	}

	if _, err := tmp.WriteString(content); err != nil {
		return fail("write", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail("set the permissions of", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("flush", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("cannot write %s: %w", p, err)
	}
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return fmt.Errorf("cannot replace %s: %w", p, err)
	}
	return nil
}

// splitLines splits content into lines without their endings and reports whether
// the last line ended with a newline — git-style diffs need to know that.
// CRLF endings are normalised away here and restored on join, so the round trip
// through ApplyEdits preserves the line endings a file predominantly uses.
func splitLines(content string) ([]string, bool) {
	if content == "" {
		return []string{""}, false
	}
	finalNL := strings.HasSuffix(content, "\n")
	body := content
	if finalNL {
		// for diff purposes the last line is the text before the final newline
		body = body[:len(body)-1]
	}
	parts := strings.Split(body, "\n")
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts, finalNL
}

// dominantEOL picks the line ending the file mostly uses (what an editor writes
// back on save); mixed endings are normalised to the dominant style, and pure LF
// files stay LF.
func dominantEOL(content string) string {
	crlf := strings.Count(content, "\r\n")
	lf := strings.Count(content, "\n") - crlf
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

// joinLines is the inverse of splitLines.
func joinLines(lines []string, eol string, finalNL bool) string {
	if len(lines) == 0 {
		return ""
	}
	out := strings.Join(lines, eol)
	if finalNL {
		out += eol
	}
	return out
}

// ApplyEdits applies line-based edits to content and returns the new content.
// Edits are applied from the bottom of the file upwards (stable sort by
// lineStart, descending), so the line numbers of every remaining edit stay valid
// after the previous ones have changed the length of the file — which is what
// lets a caller list several edits for one call in any order.
func ApplyEdits(content string, edits []LineEdit) string {
	lines, finalNL := splitLines(content)
	eol := dominantEOL(content)

	sorted := make([]LineEdit, len(edits))
	copy(sorted, edits)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, _ := strconv.Atoi(sorted[i].LineStart.String())
		b, _ := strconv.Atoi(sorted[j].LineStart.String())
		return a > b
	})

	for _, e := range sorted {
		start, _ := strconv.Atoi(e.LineStart.String())
		end, _ := strconv.Atoi(e.LineEnd.String())
		// the ranges were validated against this content by validateEdits,
		// and edits never overlap, so the mapping is exact
		var replacement []string
		if e.Text != "" {
			replacement = strings.Split(e.Text, "\n")
		}
		out := make([]string, 0, len(lines))
		out = append(out, lines[:start-1]...)
		out = append(out, replacement...)
		out = append(out, lines[end:]...)
		lines = out
	}

	return joinLines(lines, eol, finalNL)
}

// checkedEdit is a validated edit — whole numbers, ready for the overlap check.
type checkedEdit struct {
	order int // 1-based position in the request, for readable messages
	start int
	end   int
}

// validateEdits checks every edit against the original file before anything is
// written: ranges must be inside the file, ordered and non-overlapping, whole
// numbers, and a given context anchor must match. Overlapping edits are rejected
// because the result would depend on the order they are applied in.
func validateEdits(content string, edits []LineEdit) error {
	lines, _ := splitLines(content)

	checked := make([]checkedEdit, 0, len(edits))
	for i, e := range edits {
		start, err := wholeNumber(e.LineStart, "lineStart")
		if err != nil {
			return fmt.Errorf("edit %d: %s", i+1, err)
		}
		end, err := wholeNumber(e.LineEnd, "lineEnd")
		if err != nil {
			return fmt.Errorf("edit %d: %s", i+1, err)
		}
		if start < 1 {
			return fmt.Errorf("edit %d: lineStart must be >= 1 (line numbers are 1-indexed)", i+1)
		}
		if end < start {
			return fmt.Errorf("edit %d: lineEnd %d cannot be before lineStart %d", i+1, end, start)
		}
		if end > len(lines) {
			return fmt.Errorf("edit %d: lineEnd %d is out of range — the file has %d line(s)", i+1, end, len(lines))
		}

		if e.Context != nil {
			line, err := wholeNumber(e.Context.Line, "context.line")
			if err != nil {
				return fmt.Errorf("edit %d: %s", i+1, err)
			}
			if line < 1 || line > len(lines) {
				return fmt.Errorf("edit %d: context.line %d is out of range — the file has %d line(s)", i+1, line, len(lines))
			}
			if lines[line-1] != e.Context.Text {
				return fmt.Errorf("edit %d: context mismatch — line %d holds %q, the caller expected %q", i+1, line, lines[line-1], e.Context.Text)
			}
		}

		checked = append(checked, checkedEdit{order: i + 1, start: start, end: end})
	}

	sorted := make([]checkedEdit, len(checked))
	copy(sorted, checked)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].start <= sorted[i-1].end {
			return fmt.Errorf("edit %d (lines %d-%d) and edit %d (lines %d-%d) overlap — edits must cover distinct line ranges",
				sorted[i-1].order, sorted[i-1].start, sorted[i-1].end, sorted[i].order, sorted[i].start, sorted[i].end)
		}
	}
	return nil
}

// wholeNumber accepts only whole JSON numbers, so a fractional line number cannot
// slip through the "number" schema type.
func wholeNumber(n json.Number, name string) (int, error) {
	if n.String() == "" {
		return 0, fmt.Errorf("%s is required (whole 1-indexed line numbers)", name)
	}
	v, err := strconv.Atoi(n.String())
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole line number, got %s", name, n.String())
	}
	return v, nil
}

// diffOp is one line of a diff script: " " = unchanged, "-" = removed from the
// original, "+" = added by the new content. oldNo/newNo are the 1-based line
// numbers in the two files (0 when the line does not exist there) — they let the
// renderer recognise the last line of a file that has no trailing newline.
type diffOp struct {
	kind  string
	line  string
	oldNo int
	newNo int
}

// diffHunk is one hunk of a git-style diff, with the numbers for its @@ header.
type diffHunk struct {
	ops      []diffOp
	oldStart int
	oldCount int
	newStart int
	newCount int
}

// diffContext is the number of unchanged lines shown around a change — git's
// default, which is also the unified-diff standard.
const diffContext = 3

// maxDiffCells caps the LCS table: above it the diff falls back to a single
// whole-file replace hunk, which is still a correct diff, just not a minimal one.
const maxDiffCells = 4 << 20

// GenerateGitDiff renders a git-style diff between two versions of the same file:
// Index/===/---/+++ headers, @@ -old +new @@ hunk headers, and the
// "\ No newline at end of file" marker where git would put it. It returns an
// empty string when nothing changed.
func GenerateGitDiff(path, oldContent, newContent string) string {
	if oldContent == newContent {
		return ""
	}

	oldLines, oldFinal := splitLines(oldContent)
	newLines, newFinal := splitLines(newContent)

	ops := diffLines(oldLines, newLines)
	hunks := groupHunks(ops)
	if len(hunks) == 0 {
		// the lines themselves are identical — only the trailing newline differs,
		// and git reports exactly that as a one-line hunk with the marker
		last := ops[len(ops)-1]
		hunks = []diffHunk{{ops: []diffOp{last}, oldStart: last.oldNo, oldCount: 1, newStart: last.newNo, newCount: 1}}
	}

	var sb strings.Builder
	sb.WriteString("Index: " + path + "\n")
	sb.WriteString(strings.Repeat("=", 71) + "\n") // git's header separator
	sb.WriteString("--- " + path + "\n")
	sb.WriteString("+++ " + path + "\n")

	for _, h := range hunks {
		sb.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount))
		for _, op := range h.ops {
			noNewline := false
			if op.oldNo == len(oldLines) && !oldFinal && op.kind != "+" {
				noNewline = true
			}
			if op.newNo == len(newLines) && !newFinal && op.kind != "-" {
				noNewline = true
			}
			sb.WriteString(op.kind + op.line + "\n")
			if noNewline {
				sb.WriteString("\\ No newline at end of file\n")
			}
		}
	}

	return sb.String()
}

// diffLines produces the diff script by longest common subsequence (the classic
// dynamic-programming form, which yields the same minimal edit script a line-based
// diff tool produces). Above maxDiffCells it degrades to one whole-file replace.
func diffLines(oldLines, newLines []string) []diffOp {
	n, m := len(oldLines), len(newLines)

	if (n+1)*(m+1) > maxDiffCells {
		ops := make([]diffOp, 0, n+m)
		for i, l := range oldLines {
			ops = append(ops, diffOp{kind: "-", line: l, oldNo: i + 1})
		}
		for i, l := range newLines {
			ops = append(ops, diffOp{kind: "+", line: l, newNo: i + 1})
		}
		return ops
	}

	w := m + 1
	dp := make([]int32, (n+1)*(m+1))
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i*w+j] = dp[(i+1)*w+(j+1)] + 1
			} else if dp[(i+1)*w+j] >= dp[i*w+j+1] {
				dp[i*w+j] = dp[(i+1)*w+j]
			} else {
				dp[i*w+j] = dp[i*w+j+1]
			}
		}
	}

	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			ops = append(ops, diffOp{kind: " ", line: oldLines[i], oldNo: i + 1, newNo: j + 1})
			i++
			j++
		case dp[(i+1)*w+j] >= dp[i*w+j+1]:
			ops = append(ops, diffOp{kind: "-", line: oldLines[i], oldNo: i + 1})
			i++
		default:
			ops = append(ops, diffOp{kind: "+", line: newLines[j], newNo: j + 1})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{kind: "-", line: oldLines[i], oldNo: i + 1})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{kind: "+", line: newLines[j], newNo: j + 1})
	}
	return ops
}

// groupHunks packs the script into hunks with up to diffContext unchanged lines
// around each change; two change blocks separated by no more than 2*diffContext
// unchanged lines are merged into one hunk, as git and unified diff do.
func groupHunks(ops []diffOp) []diffHunk {
	var changes []int
	for i, op := range ops {
		if op.kind != " " {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		return nil
	}

	var groups [][2]int
	first, last := changes[0], changes[0]
	for _, c := range changes[1:] {
		if c-last-1 > 2*diffContext {
			groups = append(groups, [2]int{first, last})
			first = c
		}
		last = c
	}
	groups = append(groups, [2]int{first, last})

	hunks := make([]diffHunk, 0, len(groups))
	for _, g := range groups {
		lo, hi := g[0]-diffContext, g[1]+diffContext+1
		if lo < 0 {
			lo = 0
		}
		if hi > len(ops) {
			hi = len(ops)
		}
		h := diffHunk{ops: ops[lo:hi]}
		for _, op := range h.ops {
			if op.oldNo > 0 {
				if h.oldCount == 0 {
					h.oldStart = op.oldNo
				}
				h.oldCount++
			}
			if op.newNo > 0 {
				if h.newCount == 0 {
					h.newStart = op.newNo
				}
				h.newCount++
			}
		}
		hunks = append(hunks, h)
	}
	return hunks
}
