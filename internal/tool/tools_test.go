package tool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/tool"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "hello.go"), "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	mustWriteFile(t, filepath.Join(dir, "README.md"), "# Test\nThis is a test.")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mustWriteFile(t, filepath.Join(dir, "sub", "data.txt"), "line1\nline2\nline3\n")
	return dir
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
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
	mustWriteFile(t, filepath.Join(dir, "root.go"), "package main")
	mustWriteFile(t, filepath.Join(dir, "pkg", "shallow.go"), "package pkg")
	mustWriteFile(t, filepath.Join(dir, "pkg", "deep", "buried.go"), "package deep")

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

func TestScopedGlobEmptyPathUsesProjectRoot(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(home, "home.go"), "package home")
	mustWriteFile(t, filepath.Join(root, "root.go"), "package root")
	t.Setenv("HOME", home)
	t.Chdir(home)

	gt := tool.NewGlobTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "*.go"})
	result, err := gt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "root.go") {
		t.Fatalf("scoped glob output missing project file:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "home.go") {
		t.Fatalf("scoped glob leaked cwd/home file:\n%s", result.Output)
	}
}

func TestScopedGlobRejectsHomeTraversalRoot(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)

	gt := tool.NewGlobTool(root)
	input, _ := json.Marshal(map[string]any{
		"pattern": "*.go",
		"path":    home,
	})
	result, err := gt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error == nil {
		t.Fatalf("expected home traversal to be rejected, got output:\n%s", result.Output)
	}
}

func TestScopedGlobSkipsProtectedDirectories(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "safe.go"), "package safe")
	mustWriteFile(t, filepath.Join(root, "Documents", "secret.go"), "package secret")
	mustWriteFile(t, filepath.Join(root, "Library", "Mobile Documents", "icloud.go"), "package icloud")
	mustWriteFile(t, filepath.Join(root, "node_modules", "pkg.go"), "package pkg")

	gt := tool.NewGlobTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "**/*.go"})
	result, err := gt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "safe.go") {
		t.Fatalf("glob output missing safe file:\n%s", result.Output)
	}
	for _, forbidden := range []string{"secret.go", "icloud.go", "pkg.go"} {
		if strings.Contains(result.Output, forbidden) {
			t.Fatalf("glob output included protected/noisy file %q:\n%s", forbidden, result.Output)
		}
	}
}

func TestScopedGrepSkipsNestedProtectedDirectories(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "safe", "ok.txt"), "needle safe\n")
	mustWriteFile(t, filepath.Join(root, "src", "Documents", "secret.txt"), "needle secret\n")

	gt := tool.NewGrepTool(root)
	input, _ := json.Marshal(map[string]any{"pattern": "needle"})
	result, err := gt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "needle safe") {
		t.Fatalf("grep output missing safe match:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "needle secret") || strings.Contains(result.Output, "Documents") {
		t.Fatalf("grep output included nested protected directory:\n%s", result.Output)
	}
}

func TestScopedReadRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "secret.txt"), "outside secret\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	rt := tool.NewReadTool(root)
	input, _ := json.Marshal(map[string]any{"file_path": "link.txt"})
	result, err := rt.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Error == nil {
		t.Fatalf("expected symlink escape to be rejected, got:\n%s", result.Output)
	}
}

func TestScopedReadLsAndGrepUseProjectRoot(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(home, "README.md"), "home needle\n")
	mustWriteFile(t, filepath.Join(root, "README.md"), "root needle\n")
	mustWriteFile(t, filepath.Join(root, "sub", "item.txt"), "sub item\n")
	t.Chdir(home)

	rt := tool.NewReadTool(root)
	readInput, _ := json.Marshal(map[string]any{"file_path": "README.md"})
	readResult, err := rt.Execute(context.Background(), readInput)
	if err != nil {
		t.Fatalf("read Execute: %v", err)
	}
	if !strings.Contains(readResult.Output, "root needle") || strings.Contains(readResult.Output, "home needle") {
		t.Fatalf("read did not resolve relative path under root:\n%s", readResult.Output)
	}

	lt := tool.NewLsTool(root)
	lsInput, _ := json.Marshal(map[string]any{"path": "sub"})
	lsResult, err := lt.Execute(context.Background(), lsInput)
	if err != nil {
		t.Fatalf("ls Execute: %v", err)
	}
	if !strings.Contains(lsResult.Output, "item.txt") {
		t.Fatalf("ls did not resolve relative path under root:\n%s", lsResult.Output)
	}

	gt := tool.NewGrepTool(root)
	grepInput, _ := json.Marshal(map[string]any{"pattern": "needle"})
	grepResult, err := gt.Execute(context.Background(), grepInput)
	if err != nil {
		t.Fatalf("grep Execute: %v", err)
	}
	if !strings.Contains(grepResult.Output, "root needle") || strings.Contains(grepResult.Output, "home needle") {
		t.Fatalf("grep did not default to project root:\n%s", grepResult.Output)
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
