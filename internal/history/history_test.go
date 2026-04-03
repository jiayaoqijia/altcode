package history

import (
	"strings"
	"sync"
	"testing"
)

func TestNewJournal(t *testing.T) {
	j := NewJournal()
	if j == nil {
		t.Fatal("NewJournal returned nil")
	}
	if len(j.Entries()) != 0 {
		t.Fatal("new journal should be empty")
	}
}

func TestRecord_Create(t *testing.T) {
	j := NewJournal()
	j.Record("write", "/tmp/foo.txt", "create", "", "hello")

	entries := j.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Tool != "write" {
		t.Errorf("tool: got %q, want %q", e.Tool, "write")
	}
	if e.Path != "/tmp/foo.txt" {
		t.Errorf("path: got %q, want %q", e.Path, "/tmp/foo.txt")
	}
	if e.Action != "create" {
		t.Errorf("action: got %q, want %q", e.Action, "create")
	}
	if e.Before != "" {
		t.Errorf("before should be empty for create")
	}
	if e.After != "hello" {
		t.Errorf("after: got %q, want %q", e.After, "hello")
	}
	if e.Timestamp.IsZero() {
		t.Error("timestamp should be set")
	}
}

func TestRecord_Modify(t *testing.T) {
	j := NewJournal()
	j.Record("edit", "/tmp/foo.txt", "modify", "old", "new")

	entries := j.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Before != "old" || e.After != "new" {
		t.Errorf("before/after: got %q/%q", e.Before, e.After)
	}
}

func TestSummary_Mixed(t *testing.T) {
	j := NewJournal()
	j.Record("write", "/a.txt", "create", "", "a")
	j.Record("edit", "/b.txt", "modify", "b1", "b2")
	j.Record("edit", "/c.txt", "modify", "c1", "c2")
	j.Record("bash", "/d.txt", "delete", "d", "")

	s := j.Summary()
	if !strings.Contains(s, "2 modified") {
		t.Errorf("summary should have 2 modified: %q", s)
	}
	if !strings.Contains(s, "1 created") {
		t.Errorf("summary should have 1 created: %q", s)
	}
	if !strings.Contains(s, "1 deleted") {
		t.Errorf("summary should have 1 deleted: %q", s)
	}
}

func TestSummary_Empty(t *testing.T) {
	j := NewJournal()
	if j.Summary() != "no file operations" {
		t.Errorf("empty summary: %q", j.Summary())
	}
}

func TestDiff_Modify(t *testing.T) {
	j := NewJournal()
	j.Record("edit", "/tmp/test.go", "modify", "line1\nline2", "line1\nline2modified")

	d := j.diff("/tmp/test.go")
	if !strings.Contains(d, "--- a//tmp/test.go") {
		t.Errorf("diff missing old header: %q", d)
	}
	if !strings.Contains(d, "+++ b//tmp/test.go") {
		t.Errorf("diff missing new header: %q", d)
	}
	if !strings.Contains(d, "-line2") {
		t.Errorf("diff missing removed line: %q", d)
	}
	if !strings.Contains(d, "+line2modified") {
		t.Errorf("diff missing added line: %q", d)
	}
}

func TestDiff_Create(t *testing.T) {
	j := NewJournal()
	j.Record("write", "/tmp/new.go", "create", "", "package main\n")

	d := j.diff("/tmp/new.go")
	if !strings.Contains(d, "+package main") {
		t.Errorf("create diff should show added lines: %q", d)
	}
}

func TestDiff_NotFound(t *testing.T) {
	j := NewJournal()
	if j.diff("/nonexistent") != "" {
		t.Error("diff for nonexistent path should be empty")
	}
}

func TestDiff_LatestEntry(t *testing.T) {
	j := NewJournal()
	j.Record("edit", "/tmp/f.go", "modify", "v1", "v2")
	j.Record("edit", "/tmp/f.go", "modify", "v2", "v3")

	d := j.diff("/tmp/f.go")
	if !strings.Contains(d, "+v3") {
		t.Errorf("should use latest entry: %q", d)
	}
}

func TestEntriesIsCopy(t *testing.T) {
	j := NewJournal()
	j.Record("write", "/a", "create", "", "a")

	entries := j.Entries()
	entries[0].Path = "MUTATED"

	original := j.Entries()
	if original[0].Path == "MUTATED" {
		t.Error("Entries() should return a copy")
	}
}

func TestConcurrentRecord(t *testing.T) {
	j := NewJournal()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j.Record("edit", "/tmp/f.go", "modify", "a", "b")
		}()
	}
	wg.Wait()

	if len(j.Entries()) != 100 {
		t.Errorf("expected 100 entries, got %d", len(j.Entries()))
	}
}

func TestSummary_OnlyModified(t *testing.T) {
	j := NewJournal()
	j.Record("edit", "/a", "modify", "x", "y")

	s := j.Summary()
	if !strings.Contains(s, "1 modified") {
		t.Errorf("summary: %q", s)
	}
	if strings.Contains(s, "created") || strings.Contains(s, "deleted") {
		t.Errorf("should not mention create/delete: %q", s)
	}
}

func TestDiff_Delete(t *testing.T) {
	j := NewJournal()
	j.Record("bash", "/tmp/gone.go", "delete", "package main\n", "")

	d := j.diff("/tmp/gone.go")
	if !strings.Contains(d, "-package main") {
		t.Errorf("delete diff should show removed lines: %q", d)
	}
}
