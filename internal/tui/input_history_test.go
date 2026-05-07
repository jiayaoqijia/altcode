package tui

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestInputHistory_AddDedup verifies consecutive duplicates aren't
// stored — Up arrow shouldn't have to skip past three identical
// entries when the user resubmits the same prompt.
func TestInputHistory_AddDedup(t *testing.T) {
	h := newInputHistory()
	h.Add("hello")
	h.Add("hello")
	h.Add("hello")
	h.Add("world")
	if h.Len() != 2 {
		t.Errorf("Len = %d, want 2", h.Len())
	}
}

// TestInputHistory_AddTrimAndIgnoreEmpty rejects whitespace-only
// entries (avoids a bare Enter polluting history).
func TestInputHistory_AddTrimAndIgnoreEmpty(t *testing.T) {
	h := newInputHistory()
	h.Add("")
	h.Add("   \t\n  ")
	h.Add("  trimmed  ")
	if h.Len() != 1 {
		t.Fatalf("Len = %d, want 1", h.Len())
	}
	got, _ := h.Up("")
	if got != "trimmed" {
		t.Errorf("got %q, want trimmed", got)
	}
}

// TestInputHistory_UpDownNavigation covers the basic Up/Down cursor
// behaviour and the draft-restoration when the user passes the
// newest entry.
func TestInputHistory_UpDownNavigation(t *testing.T) {
	h := newInputHistory()
	for _, p := range []string{"first", "second", "third"} {
		h.Add(p)
	}
	// Start browsing from current draft.
	got, ok := h.Up("draft")
	if !ok || got != "third" {
		t.Errorf("first Up = (%q, %v), want (third, true)", got, ok)
	}
	got, _ = h.Up("")
	if got != "second" {
		t.Errorf("second Up = %q, want second", got)
	}
	got, _ = h.Up("")
	if got != "first" {
		t.Errorf("third Up = %q, want first", got)
	}
	// Down past newest restores the draft.
	got, _ = h.Down()
	if got != "second" {
		t.Errorf("Down1 = %q, want second", got)
	}
	got, _ = h.Down()
	if got != "third" {
		t.Errorf("Down2 = %q, want third", got)
	}
	got, _ = h.Down()
	if got != "draft" {
		t.Errorf("Down3 (draft restore) = %q, want draft", got)
	}
}

// TestInputHistory_UpEmptyHistory returns ok=false when nothing's
// been recorded yet.
func TestInputHistory_UpEmptyHistory(t *testing.T) {
	h := newInputHistory()
	if got, ok := h.Up("draft"); got != "" || ok {
		t.Errorf("empty Up = (%q,%v), want ('',false)", got, ok)
	}
}

// TestInputHistory_Search returns case-insensitive substring matches
// newest-first, capped at 10.
func TestInputHistory_Search(t *testing.T) {
	h := newInputHistory()
	for _, p := range []string{
		"refactor auth", "test login", "fix CI", "debug AUTH flow", "review",
	} {
		h.Add(p)
	}
	got := h.Search("auth")
	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2 — %v", len(got), got)
	}
	// Newest first.
	if got[0] != "debug AUTH flow" {
		t.Errorf("got[0] = %q, want debug AUTH flow", got[0])
	}
}

// TestInputHistory_PersistRoundtrip writes entries to disk, opens a
// fresh history backed by the same path, and confirms they survive.
func TestInputHistory_PersistRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "altcode", "history")
	h := newPersistentInputHistory(path)
	h.Add("alpha")
	h.Add("beta")

	// Reload from disk.
	h2 := newPersistentInputHistory(path)
	if h2.Len() != 2 {
		t.Fatalf("reloaded Len = %d, want 2", h2.Len())
	}
	got, _ := h2.Up("")
	if got != "beta" {
		t.Errorf("reloaded Up = %q, want beta", got)
	}
}

// TestInputHistory_Cap drops oldest entries past maxHistory so a
// long-running session doesn't grow unbounded.
func TestInputHistory_Cap(t *testing.T) {
	h := newInputHistory()
	for i := 0; i < maxHistory+25; i++ {
		h.Add(strings.Repeat("x", i+1))
	}
	if h.Len() != maxHistory {
		t.Errorf("Len = %d, want %d", h.Len(), maxHistory)
	}
}

// TestInputHistory_SkipNewlineEntriesOnSave ensures the line-oriented
// persistence format isn't corrupted by entries with embedded \n —
// they're filtered on save instead.
func TestInputHistory_SkipNewlineEntriesOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h")
	h := newPersistentInputHistory(path)
	h.Add("ok one-line")
	h.Add("multi\nline\nbad")
	h.Add("ok another")

	// Reload and confirm the multi-line entry is dropped.
	h2 := newPersistentInputHistory(path)
	if h2.Len() != 2 {
		t.Errorf("reloaded Len = %d, want 2 (multi-line dropped)", h2.Len())
	}
}
