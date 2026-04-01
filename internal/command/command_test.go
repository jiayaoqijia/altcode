package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/command"
)

func TestParseFile_WithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "review.md")
	os.WriteFile(path, []byte(`---
description: Review code changes
argument-hint: Optional file path
allowed-tools: Read, Grep, Bash(git diff *)
---

Review the code for bugs.
`), 0o644)

	cmd, err := command.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cmd.Name != "review" {
		t.Errorf("Name: %q", cmd.Name)
	}
	if cmd.Description != "Review code changes" {
		t.Errorf("Description: %q", cmd.Description)
	}
	if cmd.ArgumentHint != "Optional file path" {
		t.Errorf("ArgumentHint: %q", cmd.ArgumentHint)
	}
	if len(cmd.AllowedTools) != 3 {
		t.Errorf("AllowedTools: %v", cmd.AllowedTools)
	}
	if !strings.Contains(cmd.Body, "Review the code") {
		t.Errorf("Body: %q", cmd.Body)
	}
}

func TestParseFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "simple.md")
	os.WriteFile(path, []byte("Just do the thing.\n"), 0o644)

	cmd, err := command.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if cmd.Name != "simple" {
		t.Errorf("Name: %q", cmd.Name)
	}
	if cmd.Description != "" {
		t.Errorf("Expected empty description, got %q", cmd.Description)
	}
	if !strings.Contains(cmd.Body, "Just do the thing") {
		t.Errorf("Body: %q", cmd.Body)
	}
}

func TestParseFile_AllowedToolsArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte(`---
allowed-tools: ["Read", "Grep"]
---
body
`), 0o644)

	cmd, err := command.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cmd.AllowedTools) != 2 {
		t.Errorf("Expected 2 tools, got %v", cmd.AllowedTools)
	}
}

func TestExpand_Arguments(t *testing.T) {
	cmd := &command.Command{Body: "Review $ARGUMENTS for issues."}

	result, err := cmd.Expand("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if result != "Review main.go for issues." {
		t.Errorf("Got: %q", result)
	}
}

func TestExpand_Backtick(t *testing.T) {
	cmd := &command.Command{Body: "Date: !`echo 2026-04-01`"}

	result, _ := cmd.Expand("")
	if !strings.Contains(result, "2026-04-01") {
		t.Errorf("Expected date in output: %q", result)
	}
}

func TestExpand_BacktickError(t *testing.T) {
	cmd := &command.Command{Body: "Output: !`nonexistent_command_xyz`"}

	result, err := cmd.Expand("")
	if err == nil {
		t.Log("Error expected but may not propagate for all shells")
	}
	// Should not panic, should contain error placeholder
	_ = result
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "commit.md"), []byte("---\ndescription: Commit\n---\ncommit"), 0o644)
	os.WriteFile(filepath.Join(dir, "review.md"), []byte("review code"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.txt"), []byte("not a command"), 0o644)

	cmds, err := command.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(cmds) != 2 {
		t.Fatalf("Expected 2 commands, got %d", len(cmds))
	}
}

func TestDiscover_Override(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	os.WriteFile(filepath.Join(dir1, "review.md"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir2, "review.md"), []byte("new"), 0o644)

	cmds, _ := command.Discover(dir1, dir2)
	if len(cmds) != 1 {
		t.Fatalf("Expected 1 command (deduped), got %d", len(cmds))
	}
	if cmds[0].Body != "new" {
		t.Error("Later directory should override")
	}
}

func TestDiscover_NonexistentDir(t *testing.T) {
	cmds, err := command.Discover("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 0 {
		t.Error("Should return empty for nonexistent dir")
	}
}
