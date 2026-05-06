package memory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/memory"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	err := s.Save("user-prefs", "User Preferences", "Prefers Go. Uses vim.")
	if err != nil {
		t.Fatal(err)
	}

	m, err := s.Load("user-prefs")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "User Preferences" {
		t.Errorf("Title: %q", m.Title)
	}
	if !strings.Contains(m.Content, "Prefers Go") {
		t.Errorf("Content: %q", m.Content)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("mem1", "First", "content one")
	s.Save("mem2", "Second", "content two")

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("Expected 2, got %d", len(list))
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("temp", "Temp", "temporary")
	err := s.Delete("temp")
	if err != nil {
		t.Fatal(err)
	}

	list, _ := s.List()
	if len(list) != 0 {
		t.Error("Should be empty after delete")
	}
}

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("go-prefs", "Go Preferences", "Use gofmt. Prefer short functions.")
	s.Save("py-prefs", "Python Preferences", "Use black formatter.")

	results, _ := s.Search("gofmt")
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
	if results[0].ID != "go-prefs" {
		t.Errorf("Wrong result: %q", results[0].ID)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("test", "Test", "Use UPPERCASE patterns")

	results, _ := s.Search("uppercase")
	if len(results) != 1 {
		t.Error("Search should be case-insensitive")
	}
}

func TestForContext(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("ctx1", "Context One", "First piece of context")
	s.Save("ctx2", "Context Two", "Second piece of context")

	ctx := s.ForContext(10000)
	if !strings.Contains(ctx, "# Memories") {
		t.Error("Should have header")
	}
	if !strings.Contains(ctx, "First piece") {
		t.Error("Should contain first memory")
	}
	if !strings.Contains(ctx, "Second piece") {
		t.Error("Should contain second memory")
	}
}

func TestForContextTruncation(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	// Create many large memories
	for i := 0; i < 20; i++ {
		s.Save(
			strings.Repeat("a", 5)+string(rune('a'+i)),
			"Big Memory",
			strings.Repeat("x", 5000),
		)
	}

	ctx := s.ForContext(1000)
	if len(ctx) > 1200 {
		t.Errorf("Should truncate, got %d bytes", len(ctx))
	}
	if !strings.Contains(ctx, "truncated") {
		t.Error("Should indicate truncation")
	}
}

func TestForContextEmpty(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	ctx := s.ForContext(10000)
	if ctx != "" {
		t.Error("Empty store should return empty string")
	}
}

func TestIndex(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("mem1", "First Memory", "details about first")
	s.Save("mem2", "Second Memory", "details about second")

	idx, err := s.LoadIndex()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(idx, "First Memory") {
		t.Error("Index should contain first memory")
	}
	if !strings.Contains(idx, "Second Memory") {
		t.Error("Index should contain second memory")
	}
	if !strings.Contains(idx, ".md") {
		t.Error("Index should link to .md files")
	}
}

func TestClaudeCodeMemoryFormat(t *testing.T) {
	// Test that memories written in Claude Code's format can be read
	dir := t.TempDir()

	// Write in Claude Code's format
	os.WriteFile(filepath.Join(dir, "project-context.md"), []byte(`---
name: Project Context
description: altcode is a Go CLI for AI-assisted coding
type: project
---

altcode is a Go CLI/TUI for AI-assisted coding.
Key patterns: channel pipeline, tool dispatch, hook system.
`), 0o644)

	s := memory.NewStore(dir)
	m, err := s.Load("project-context")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Project Context" {
		t.Errorf("Title: %q", m.Title)
	}
	if m.Summary != "altcode is a Go CLI for AI-assisted coding" {
		t.Errorf("Summary: %q", m.Summary)
	}
	if !strings.Contains(m.Content, "channel pipeline") {
		t.Error("Content should be body after frontmatter")
	}
}

func TestListEmptyDir(t *testing.T) {
	s := memory.NewStore("/nonexistent/dir")
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Error("Should return empty for nonexistent dir")
	}
}

func TestOverwriteExistingMemory(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("key1", "Original", "original content")
	s.Save("key1", "Updated", "updated content")

	m, err := s.Load("key1")
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Updated" {
		t.Errorf("Title should be updated: %q", m.Title)
	}
	if !strings.Contains(m.Content, "updated content") {
		t.Errorf("Content should be updated: %q", m.Content)
	}

	// Should still have only 1 memory
	list, _ := s.List()
	if len(list) != 1 {
		t.Errorf("Expected 1 memory after overwrite, got %d", len(list))
	}
}

func TestLoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	_, err := s.Load("nonexistent-key")
	if err == nil {
		t.Error("Expected error for nonexistent memory")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	// Deleting nonexistent returns an error (file not found)
	err := s.Delete("nonexistent-key")
	if err == nil {
		t.Error("Expected error when deleting nonexistent memory")
	}
}

func TestSearchNoResults(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("test", "Test", "hello world")
	results, _ := s.Search("zzz_nonexistent_pattern")
	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

func TestSearchInTitle(t *testing.T) {
	dir := t.TempDir()
	s := memory.NewStore(dir)

	s.Save("test", "Important Project Notes", "just some text")
	results, _ := s.Search("Important Project")
	if len(results) != 1 {
		t.Errorf("Expected 1 result searching title, got %d", len(results))
	}
}

func TestDefaultDirs(t *testing.T) {
	d1 := memory.DefaultDir("/project")
	if !strings.Contains(d1, ".altcode") {
		t.Errorf("Default should use .altcode: %q", d1)
	}
	d2 := memory.ClaudeCodeDir("/project")
	if !strings.Contains(d2, ".claude") {
		t.Errorf("Claude Code should use .claude: %q", d2)
	}
}
