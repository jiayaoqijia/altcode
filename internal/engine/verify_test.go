package engine

import (
	"context"
	"testing"
	"time"
)

func TestVerifyLevelName(t *testing.T) {
	tests := []struct {
		level VerifyLevel
		want  string
	}{
		{VerifyBuild, "build"},
		{VerifyVet, "vet"},
		{VerifyTest, "test (targeted)"},
		{VerifyPackage, "test (package)"},
		{VerifyLint, "lint"},
		{VerifyLevel(99), "unknown"},
	}
	for _, tt := range tests {
		got := verifyLevelName(tt.level)
		if got != tt.want {
			t.Errorf("verifyLevelName(%d) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestDefaultVerificationLadder(t *testing.T) {
	ladder := DefaultVerificationLadder()
	if len(ladder) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(ladder))
	}
	if ladder[0] != VerifyBuild || ladder[1] != VerifyVet || ladder[2] != VerifyTest {
		t.Errorf("unexpected ladder order: %v", ladder)
	}
}

func TestFormatVerificationResults_Empty(t *testing.T) {
	got := FormatVerificationResults(nil)
	if got != "no verification run" {
		t.Errorf("got %q, want %q", got, "no verification run")
	}
}

func TestFormatVerificationResults_Mixed(t *testing.T) {
	results := []VerifyResult{
		{Level: VerifyBuild, Passed: true, Elapsed: 50 * time.Millisecond},
		{Level: VerifyVet, Passed: false, Output: "vet error", Elapsed: 120 * time.Millisecond},
	}
	out := FormatVerificationResults(results)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	// Check for pass/fail icons
	if !contains(out, "✓") || !contains(out, "✗") {
		t.Error("expected ✓ and ✗ icons in output")
	}
	if !contains(out, "build") || !contains(out, "vet") {
		t.Error("expected level names in output")
	}
	if !contains(out, "vet error") {
		t.Error("expected failure output in result")
	}
}

func TestRunVerificationLadder_StopsAtFailure(t *testing.T) {
	// Use a nonexistent directory so build fails immediately
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results := RunVerificationLadder(ctx, "/nonexistent/path", DefaultVerificationLadder())
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	// First step should fail (nonexistent dir)
	if results[0].Passed {
		t.Error("expected first step to fail for nonexistent dir")
	}
	// Should stop at first failure
	if len(results) > 1 {
		t.Errorf("expected 1 result (stop at failure), got %d", len(results))
	}
}

func TestRunVerificationLadder_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	results := RunVerificationLadder(ctx, ".", DefaultVerificationLadder())
	// Should fail quickly due to cancelled context
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Passed {
		t.Error("expected failure with cancelled context")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
