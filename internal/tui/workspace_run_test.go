package tui

import (
	"testing"
)

// TestParseWorkspaceArgs_NoSpecsAutoDetect when no token has a known
// backend prefix, the task captures all tokens and specs is nil.
func TestParseWorkspaceArgs_NoSpecsAutoDetect(t *testing.T) {
	parts := []string{"add", "auth", "to", "the", "API"}
	task, specs := parseWorkspaceArgs(parts)
	if task != "add auth to the API" {
		t.Errorf("task = %q, want 'add auth to the API'", task)
	}
	if specs != nil {
		t.Errorf("specs = %v, want nil", specs)
	}
}

// TestParseWorkspaceArgs_RecognizedSpecs splits known backend:role
// tokens out of the task while preserving order of remaining tokens.
func TestParseWorkspaceArgs_RecognizedSpecs(t *testing.T) {
	parts := []string{"add", "auth", "claude:architect", "codex:coder"}
	task, specs := parseWorkspaceArgs(parts)
	if task != "add auth" {
		t.Errorf("task = %q, want 'add auth'", task)
	}
	if len(specs) != 2 {
		t.Fatalf("len(specs) = %d, want 2", len(specs))
	}
	if specs[0].backend != "claude" || specs[0].role != "architect" {
		t.Errorf("spec[0] = %+v", specs[0])
	}
	if specs[1].backend != "codex" || specs[1].role != "coder" {
		t.Errorf("spec[1] = %+v", specs[1])
	}
}

// TestParseWorkspaceArgs_PreservesURLs is the bug-regression case:
// "https://example.com" must NOT be classified as backend "https". Was
// silently dropping the URL from the task before the knownBackends
// allowlist landed.
func TestParseWorkspaceArgs_PreservesURLs(t *testing.T) {
	parts := []string{"fix", "https://example.com", "outage"}
	task, specs := parseWorkspaceArgs(parts)
	if task != "fix https://example.com outage" {
		t.Errorf("URL dropped from task: %q", task)
	}
	if specs != nil {
		t.Errorf("specs = %v, want nil for non-backend colon", specs)
	}
}

// TestParseWorkspaceArgs_RejectsUnknownBackend ensures only the
// allowlisted backends are recognized.
func TestParseWorkspaceArgs_RejectsUnknownBackend(t *testing.T) {
	parts := []string{"foo:bar", "task"}
	task, specs := parseWorkspaceArgs(parts)
	if task != "foo:bar task" {
		t.Errorf("unknown backend should pass through: task=%q", task)
	}
	if specs != nil {
		t.Error("specs should be nil for unknown backend")
	}
}

// TestParseWorkspaceArgs_EmptyKVPartsHandled exercises the missing-
// LHS / missing-RHS guard. "claude:" or ":role" should not pass.
func TestParseWorkspaceArgs_EmptyKVPartsHandled(t *testing.T) {
	cases := [][]string{
		{"claude:", "task"},
		{":role", "task"},
	}
	for _, parts := range cases {
		_, specs := parseWorkspaceArgs(parts)
		if specs != nil {
			t.Errorf("input %v should not produce specs, got %v", parts, specs)
		}
	}
}

// TestKnownWorkspaceBackends_HasFourEntries is a registry-level
// sanity test: any addition must be intentional and visible in code
// review. If this fails, decide whether the change is meant.
func TestKnownWorkspaceBackends_HasFourEntries(t *testing.T) {
	want := map[string]bool{
		"claude":   true,
		"codex":    true,
		"opencode": true,
		"altcode":  true,
	}
	if len(knownWorkspaceBackends) != len(want) {
		t.Errorf("len = %d, want %d", len(knownWorkspaceBackends), len(want))
	}
	for k := range want {
		if !knownWorkspaceBackends[k] {
			t.Errorf("missing backend %q", k)
		}
	}
}
