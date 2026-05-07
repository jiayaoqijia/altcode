package tool

import (
	"strings"
	"testing"
)

// TestApplyHunk_RejectsContextMismatch guards against the Codex
// round-J finding: the fallback applier previously trusted line
// numbers blindly and silently rewrote wrong lines when the diff
// context didn't match. It must now refuse a stale diff.
func TestApplyHunk_RejectsContextMismatch(t *testing.T) {
	lines := []string{"alpha", "beta", "gamma", "delta"}
	h := hunk{
		OldStart: 1,
		Lines: []diffLine{
			{Op: ' ', Text: "alpha"},
			{Op: ' ', Text: "BETA_MISMATCH"}, // real file has "beta"
			{Op: '-', Text: "gamma"},
			{Op: '+', Text: "GAMMA_NEW"},
		},
	}
	_, err := applyHunk(lines, h)
	if err == nil {
		t.Fatal("expected context-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "context mismatch") {
		t.Errorf("err = %v, want mention of context mismatch", err)
	}
}

// TestApplyHunk_RejectsDeleteMismatch ensures a `-` line whose text
// doesn't match the file also fails loudly rather than corrupting.
func TestApplyHunk_RejectsDeleteMismatch(t *testing.T) {
	lines := []string{"a", "b", "c"}
	h := hunk{
		OldStart: 1,
		Lines: []diffLine{
			{Op: ' ', Text: "a"},
			{Op: '-', Text: "WRONG"}, // real file has "b"
		},
	}
	_, err := applyHunk(lines, h)
	if err == nil || !strings.Contains(err.Error(), "delete mismatch") {
		t.Errorf("expected delete-mismatch error, got %v", err)
	}
}

// TestApplyHunk_HappyPathStillWorks regression-guards the fix.
func TestApplyHunk_HappyPathStillWorks(t *testing.T) {
	lines := []string{"alpha", "beta", "gamma"}
	h := hunk{
		OldStart: 1,
		Lines: []diffLine{
			{Op: ' ', Text: "alpha"},
			{Op: '-', Text: "beta"},
			{Op: '+', Text: "BETA2"},
			{Op: ' ', Text: "gamma"},
		},
	}
	out, err := applyHunk(lines, h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := strings.Join(out, "|")
	if got != "alpha|BETA2|gamma" {
		t.Errorf("got %q, want %q", got, "alpha|BETA2|gamma")
	}
}
