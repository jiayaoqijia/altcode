package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/tool"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\nThis is a test."), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "data.txt"), []byte("line1\nline2\nline3\n"), 0o644)
	return dir
}

func TestReadTool(t *testing.T) {
	dir := setupTestDir(t)
	rt := tool.NewReadTool()

	input, _ := json.Marshal(map[string]any{"file_path": filepath.Join(dir, "hello.go")})
	result, err := rt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output == "" {
		t.Fatal("Expected non-empty output")
	}
	if result.Title == "" {
		t.Fatal("Expected non-empty title")
	}
}

func TestReadToolWithLineRange(t *testing.T) {
	dir := setupTestDir(t)
	rt := tool.NewReadTool()

	input, _ := json.Marshal(map[string]any{
		"file_path": filepath.Join(dir, "sub", "data.txt"),
		"offset":    1,
		"limit":     2,
	})
	result, err := rt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	t.Logf("Output: %s", result.Output)
}

func TestGlobTool(t *testing.T) {
	dir := setupTestDir(t)
	gt := tool.NewGlobTool()

	input, _ := json.Marshal(map[string]any{"pattern": "*.go", "path": dir})
	result, err := gt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output == "" {
		t.Fatal("Expected matches")
	}
	t.Logf("Glob output: %s", result.Output)
}

// TestGlobTool_DoubleStar is a regression test: the tool description
// promises **/*.go works, but the previous matcher used filepath.Match
// against the basename only, returning zero matches for any **-prefixed
// pattern. With matchGlob the pattern must find the nested .txt file.
func TestGlobTool_DoubleStar(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "root.go"), []byte("package main"), 0o644)
	os.MkdirAll(filepath.Join(dir, "pkg", "deep"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "shallow.go"), []byte("package pkg"), 0o644)
	os.WriteFile(filepath.Join(dir, "pkg", "deep", "buried.go"), []byte("package deep"), 0o644)

	tests := []struct {
		name    string
		pattern string
		want    []string // expected substrings in output
	}{
		{"flat star", "*.go", []string{"root.go"}},
		{"recursive double-star", "**/*.go", []string{"root.go", "shallow.go", "buried.go"}},
		{"prefixed double-star", "pkg/**/*.go", []string{"shallow.go", "buried.go"}},
	}
	gt := tool.NewGlobTool()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(map[string]any{
				"pattern": tt.pattern,
				"path":    dir,
			})
			result, err := gt.Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(result.Output, want) {
					t.Errorf("pattern %q: output missing %q\nfull:\n%s",
						tt.pattern, want, result.Output)
				}
			}
		})
	}
}

func TestLsTool(t *testing.T) {
	dir := setupTestDir(t)
	lt := tool.NewLsTool()

	input, _ := json.Marshal(map[string]any{"path": dir})
	result, err := lt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output == "" {
		t.Fatal("Expected directory listing")
	}
	t.Logf("Ls output: %s", result.Output)
}

func TestBashTool(t *testing.T) {
	bt := tool.NewBashTool()
	input, _ := json.Marshal(map[string]any{"command": "echo hello"})
	result, err := bt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Fatalf("Expected output to contain 'hello', got %q", result.Output)
	}
}

// A command killed by its own timeout must surface a clear annotation
// so the agent knows to retry with a larger `timeout` parameter instead
// of interpreting the exit as a script bug.
func TestBashTool_TimeoutAnnotation(t *testing.T) {
	bt := tool.NewBashTool()
	input, _ := json.Marshal(map[string]any{
		"command": "sleep 5",
		"timeout": 100, // 100ms — will trip
	})
	result, err := bt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Output, "killed") || !strings.Contains(result.Output, "timeout") {
		t.Fatalf("expected timeout annotation in output, got %q", result.Output)
	}
}

// Error-path tools must set Result.Error so the tree renders a red ✗
// instead of a misleading green ✓. Previously `Read /nonexistent` came
// back with `Error: ...` in Output but nil Error field, so the TUI
// cheerfully showed it as successful.
func TestReadTool_ErrorFieldSetOnFailure(t *testing.T) {
	rt := tool.NewReadTool()
	input, _ := json.Marshal(map[string]any{"file_path": "/nonexistent/no-such-file.txt"})
	result, err := rt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error == nil {
		t.Fatalf("expected Result.Error set on missing file; output was %q", result.Output)
	}
	if !strings.Contains(result.Output, "no such file") {
		t.Errorf("expected 'no such file' in output, got %q", result.Output)
	}
}

func TestLsTool_ErrorFieldSetOnFailure(t *testing.T) {
	lt := tool.NewLsTool()
	input, _ := json.Marshal(map[string]any{"path": "/nonexistent/no-such-dir"})
	result, err := lt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error == nil {
		t.Fatalf("expected Result.Error set on missing dir; output was %q", result.Output)
	}
}

func TestEditTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world\nfoo bar\n"), 0o644)

	et := tool.NewEditTool()
	input, _ := json.Marshal(map[string]any{
		"file_path":  path,
		"old_string": "hello world",
		"new_string": "hello altcode",
	})
	result, err := et.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("Tool error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "hello altcode") {
		t.Fatalf("Expected 'hello altcode', got %q", string(data))
	}
}

func TestWriteTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")

	wt := tool.NewWriteTool()
	input, _ := json.Marshal(map[string]any{
		"file_path": path,
		"content":   "brand new file\n",
	})
	result, err := wt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("Tool error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "brand new file\n" {
		t.Fatalf("Expected 'brand new file\\n', got %q", string(data))
	}
}
