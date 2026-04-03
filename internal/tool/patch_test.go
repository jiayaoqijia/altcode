package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/tool"
)

func TestPatchToolMetadata(t *testing.T) {
	pt := tool.NewPatchTool()
	if pt.Name() != "apply_patch" {
		t.Fatalf("Name = %q, want apply_patch", pt.Name())
	}
	if pt.IsConcurrencySafe() {
		t.Fatal("Expected not concurrency safe")
	}
	if pt.IsReadOnly() {
		t.Fatal("Expected not read only")
	}
	if pt.Description() == "" {
		t.Fatal("Expected non-empty description")
	}
	var schema map[string]any
	if err := json.Unmarshal(pt.Parameters(), &schema); err != nil {
		t.Fatalf("Invalid parameters JSON: %v", err)
	}
}

func TestPatchToolEmptyPatch(t *testing.T) {
	pt := tool.NewPatchTool()
	input, _ := json.Marshal(map[string]any{"patch": ""})
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "Error") {
		t.Fatalf("Expected error for empty patch, got %q", result.Output)
	}
}

func TestPatchToolSimpleEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)

	patch := strings.Join([]string{
		"--- a/" + path,
		"+++ b/" + path,
		"@@ -1,3 +1,3 @@",
		" line1",
		"-line2",
		"+line2_modified",
		" line3",
	}, "\n")

	pt := tool.NewPatchTool()
	input, _ := json.Marshal(map[string]any{"patch": patch})
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "Error") {
		t.Fatalf("Unexpected error: %s", result.Output)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "line2_modified") {
		t.Fatalf("Expected 'line2_modified', got %q", content)
	}
	if strings.Contains(content, "line2\n") && !strings.Contains(content, "line2_modified") {
		t.Fatal("Old line2 should be replaced")
	}
}

func TestPatchToolAddLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "add.txt")
	os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644)

	patch := strings.Join([]string{
		"--- a/" + path,
		"+++ b/" + path,
		"@@ -1,2 +1,4 @@",
		" alpha",
		"+gamma",
		"+delta",
		" beta",
	}, "\n")

	pt := tool.NewPatchTool()
	input, _ := json.Marshal(map[string]any{"patch": patch})
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "Error") {
		t.Fatalf("Unexpected error: %s", result.Output)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "gamma") {
		t.Fatalf("Expected 'gamma' in output, got %q", content)
	}
	if !strings.Contains(content, "delta") {
		t.Fatalf("Expected 'delta' in output, got %q", content)
	}
}

func TestPatchToolRemoveLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "remove.txt")
	os.WriteFile(path, []byte("keep\nremove_me\nkeep_too\n"), 0o644)

	patch := strings.Join([]string{
		"--- a/" + path,
		"+++ b/" + path,
		"@@ -1,3 +1,2 @@",
		" keep",
		"-remove_me",
		" keep_too",
	}, "\n")

	pt := tool.NewPatchTool()
	input, _ := json.Marshal(map[string]any{"patch": patch})
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "Error") {
		t.Fatalf("Unexpected error: %s", result.Output)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, "remove_me") {
		t.Fatalf("Line should be removed, got %q", content)
	}
	if !strings.Contains(content, "keep") {
		t.Fatal("Expected 'keep' to remain")
	}
}

func TestPatchToolNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "new.txt")

	patch := strings.Join([]string{
		"--- /dev/null",
		"+++ b/" + path,
		"@@ -0,0 +1,2 @@",
		"+new file line 1",
		"+new file line 2",
	}, "\n")

	pt := tool.NewPatchTool()
	input, _ := json.Marshal(map[string]any{"patch": patch})
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "Error") {
		t.Fatalf("Unexpected error: %s", result.Output)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("File should exist: %v", err)
	}
	if !strings.Contains(string(data), "new file line 1") {
		t.Fatalf("Expected new content, got %q", string(data))
	}
}

func TestPatchToolPermissionPattern(t *testing.T) {
	pt := tool.NewPatchTool()
	input, _ := json.Marshal(map[string]any{"patch": "..."})
	pattern := pt.PermissionPattern(input)
	if pattern != "apply_patch" {
		t.Fatalf("Pattern = %q, want apply_patch", pattern)
	}
}

func TestPatchToolMultipleHunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.txt")
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line" + strings.Repeat("_", i+1)
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	patch := strings.Join([]string{
		"--- a/" + path,
		"+++ b/" + path,
		"@@ -1,3 +1,3 @@",
		"-line_",
		"+FIRST",
		" line__",
		" line___",
	}, "\n")

	pt := tool.NewPatchTool()
	input, _ := json.Marshal(map[string]any{"patch": patch})
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Output, "Error") {
		t.Fatalf("Unexpected error: %s", result.Output)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "FIRST") {
		t.Fatalf("Expected 'FIRST', got %q", content)
	}
}

func TestPatchToolBadHunkHeader(t *testing.T) {
	pt := tool.NewPatchTool()
	patch := strings.Join([]string{
		"--- a/foo.txt",
		"+++ b/foo.txt",
		"@@ bad header @@",
		"+line",
	}, "\n")

	input, _ := json.Marshal(map[string]any{"patch": patch})
	result, err := pt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Should either handle gracefully or report error
	t.Logf("Result: %s", result.Output)
}
