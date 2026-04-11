package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type patchTool struct{}

// NewPatchTool creates a tool that applies unified diff patches.
func NewPatchTool() Tool { return &patchTool{} }

func (t *patchTool) Name() string { return "apply_patch" }
func (t *patchTool) Description() string {
	return "Apply a unified diff patch to one or more files. More robust than edit for multi-line changes."
}
func (t *patchTool) IsConcurrencySafe() bool { return false }
func (t *patchTool) IsReadOnly() bool        { return false }
func (t *patchTool) PermissionPattern(input json.RawMessage) string {
	return "apply_patch"
}

func (t *patchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"patch": {"type": "string", "description": "Unified diff patch content"}
		},
		"required": ["patch"]
	}`)
}

func (t *patchTool) Execute(ctx context.Context, input json.RawMessage) (*Result, error) {
	var params struct {
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, fmt.Errorf("parse input: %w", err)
	}
	if params.Patch == "" {
		return &Result{
			Output: "Error: patch is required",
			Title:  "apply_patch",
			Error:  fmt.Errorf("patch is required"),
		}, nil
	}

	// Try system patch command first
	if result := trySystemPatch(ctx, params.Patch); result != nil {
		return result, nil
	}

	// Fallback: parse and apply manually
	return applyPatchManual(params.Patch)
}

func trySystemPatch(ctx context.Context, patch string) *Result {
	// Skip system patch for new files — macOS patch handles them poorly
	if strings.Contains(patch, "--- /dev/null") {
		return nil
	}

	patchBin, err := exec.LookPath("patch")
	if err != nil {
		return nil // not available, use fallback
	}

	cmd := exec.CommandContext(ctx, patchBin, "-p1", "--no-backup-if-mismatch")
	cmd.Stdin = strings.NewReader(patch)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil // failed, try manual fallback
	}

	output := stdout.String()
	if stderr.Len() > 0 && output != "" {
		output += "\n" + stderr.String()
	}
	return &Result{
		Output: "Patch applied successfully.\n" + output,
		Title:  "apply_patch",
	}
}

func applyPatchManual(patch string) (*Result, error) {
	files, err := parsePatch(patch)
	if err != nil {
		// Result.Error must be set so the agent loop sees this as a
		// failed tool call. Without it, the model thinks the patch
		// was applied and continues to the next step.
		return &Result{
			Output: fmt.Sprintf("Error parsing patch: %v", err),
			Title:  "apply_patch",
			Error:  fmt.Errorf("parse patch: %w", err),
		}, nil
	}

	var applied []string
	for _, pf := range files {
		if err := applyFilePatch(pf); err != nil {
			return &Result{
				Output: fmt.Sprintf("Error applying patch to %s: %v", pf.Path, err),
				Title:  "apply_patch",
				Error:  fmt.Errorf("apply %s: %w", pf.Path, err),
			}, nil
		}
		applied = append(applied, pf.Path)
	}

	return &Result{
		Output: fmt.Sprintf("Patch applied to %d file(s): %s",
			len(applied), strings.Join(applied, ", ")),
		Title: "apply_patch",
	}, nil
}

type patchFile struct {
	Path  string
	Hunks []hunk
}

type hunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []diffLine
}

type diffLine struct {
	Op   byte // ' ', '+', '-'
	Text string
}

func parsePatch(patch string) ([]patchFile, error) {
	lines := strings.Split(patch, "\n")
	var files []patchFile
	var current *patchFile
	var curHunk *hunk

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if strings.HasPrefix(line, "--- ") {
			// Start of a new file diff
			if current != nil {
				if curHunk != nil {
					current.Hunks = append(current.Hunks, *curHunk)
					curHunk = nil
				}
				files = append(files, *current)
			}
			current = &patchFile{}
			curHunk = nil
			continue
		}

		if strings.HasPrefix(line, "+++ ") {
			if current == nil {
				current = &patchFile{}
			}
			path := strings.TrimPrefix(line, "+++ ")
			// Strip a/ or b/ prefix
			path = stripPathPrefix(path)
			current.Path = path
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			if curHunk != nil && current != nil {
				current.Hunks = append(current.Hunks, *curHunk)
			}
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf("bad hunk header %q: %w", line, err)
			}
			curHunk = h
			continue
		}

		if curHunk == nil {
			continue // skip diff header lines
		}

		if len(line) == 0 {
			// Treat empty lines in a hunk as context lines
			curHunk.Lines = append(curHunk.Lines, diffLine{Op: ' '})
			continue
		}

		op := line[0]
		text := line[1:]
		switch op {
		case ' ', '+', '-':
			curHunk.Lines = append(curHunk.Lines, diffLine{Op: op, Text: text})
		case '\\':
			// "\ No newline at end of file" — skip
		default:
			// Treat as context
			curHunk.Lines = append(curHunk.Lines, diffLine{Op: ' ', Text: line})
		}
	}

	if current != nil {
		if curHunk != nil {
			current.Hunks = append(current.Hunks, *curHunk)
		}
		files = append(files, *current)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found in patch")
	}
	return files, nil
}

func stripPathPrefix(path string) string {
	// Remove "a/" or "b/" prefix common in git diffs
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		return path[2:]
	}
	return path
}

func parseHunkHeader(line string) (*hunk, error) {
	// Format: @@ -old_start,old_count +new_start,new_count @@
	line = strings.TrimPrefix(line, "@@ ")
	idx := strings.Index(line, " @@")
	if idx >= 0 {
		line = line[:idx]
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil, fmt.Errorf("expected 2 parts, got %d", len(parts))
	}

	h := &hunk{}
	old := strings.TrimPrefix(parts[0], "-")
	neu := strings.TrimPrefix(parts[1], "+")

	oldParts := strings.SplitN(old, ",", 2)
	h.OldStart, _ = strconv.Atoi(oldParts[0])
	if len(oldParts) > 1 {
		h.OldCount, _ = strconv.Atoi(oldParts[1])
	} else {
		h.OldCount = 1
	}

	newParts := strings.SplitN(neu, ",", 2)
	h.NewStart, _ = strconv.Atoi(newParts[0])
	if len(newParts) > 1 {
		h.NewCount, _ = strconv.Atoi(newParts[1])
	} else {
		h.NewCount = 1
	}

	return h, nil
}

func applyFilePatch(pf patchFile) error {
	if pf.Path == "/dev/null" {
		return nil
	}

	// Create parent directories if needed
	dir := filepath.Dir(pf.Path)
	if dir != "." && dir != "" {
		os.MkdirAll(dir, 0o755)
	}

	// Read existing content (empty for new files)
	data, _ := os.ReadFile(pf.Path)
	lines := splitLines(string(data))

	for _, h := range pf.Hunks {
		var err error
		lines, err = applyHunk(lines, h)
		if err != nil {
			return fmt.Errorf("hunk at line %d: %w", h.OldStart, err)
		}
	}

	result := joinLines(lines)
	return os.WriteFile(pf.Path, []byte(result), 0o644)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

func applyHunk(lines []string, h hunk) ([]string, error) {
	// Convert to 0-indexed
	start := h.OldStart - 1
	if start < 0 {
		start = 0
	}

	// Build the new content for this region
	var newLines []string
	pos := start

	for _, dl := range h.Lines {
		switch dl.Op {
		case ' ':
			// Context line — keep it, advance position
			if pos < len(lines) {
				newLines = append(newLines, lines[pos])
			}
			pos++
		case '-':
			// Remove line — skip it
			pos++
		case '+':
			// Add line
			newLines = append(newLines, dl.Text)
		}
	}

	// Build result: lines before hunk + new lines + lines after hunk
	result := make([]string, 0, len(lines)+len(newLines))
	result = append(result, lines[:start]...)
	result = append(result, newLines...)
	if pos < len(lines) {
		result = append(result, lines[pos:]...)
	}

	return result, nil
}
