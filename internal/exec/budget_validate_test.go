package exec

import (
	"strings"
	"testing"
)

// TestParams_Validate_MaxTurnsNegative ensures Validate rejects
// negative max_turns with a UsageError (exit 64). Autoresearch
// iteration 3 coverage per Codex re-review: budget semantics
// should be proven end-to-end, starting at the Params boundary.
func TestParams_Validate_MaxTurnsNegative(t *testing.T) {
	p := &Params{MaxTurns: -1, Prompt: "hi"}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected UsageError for negative max_turns")
	}
	var ue *UsageError
	if !asUsageError(err, &ue) {
		t.Fatalf("err is not UsageError: %T", err)
	}
	if !strings.Contains(err.Error(), "--max-turns") {
		t.Errorf("error should mention --max-turns: %q", err)
	}
}

// TestParams_Validate_MaxCostNegative ensures Validate rejects
// negative max_cost.
func TestParams_Validate_MaxCostNegative(t *testing.T) {
	p := &Params{MaxCost: -0.5, Prompt: "hi"}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected UsageError for negative max_cost")
	}
	if !strings.Contains(err.Error(), "--max-cost") {
		t.Errorf("error should mention --max-cost: %q", err)
	}
}

// TestParams_Validate_MaxTurnsZeroAllowed verifies 0 (the "no limit,
// use engine default" convention) does NOT trigger the validator.
func TestParams_Validate_MaxTurnsZeroAllowed(t *testing.T) {
	p := &Params{MaxTurns: 0, Prompt: "hi"}
	if err := p.Validate(); err != nil {
		// The test's intent: MaxTurns=0 must not be the reason Validate
		// fails. Other fields may trigger unrelated errors, but never
		// "--max-turns".
		if strings.Contains(err.Error(), "--max-turns") {
			t.Errorf("MaxTurns=0 should be allowed; got: %v", err)
		}
	}
}

// TestParams_Validate_MaxTurnsPositive verifies a reasonable positive
// max_turns is accepted without the budget validator complaining.
func TestParams_Validate_MaxTurnsPositive(t *testing.T) {
	p := &Params{MaxTurns: 42, MaxCost: 1.25, Prompt: "hi"}
	if err := p.Validate(); err != nil {
		if strings.Contains(err.Error(), "--max-turns") ||
			strings.Contains(err.Error(), "--max-cost") {
			t.Errorf("positive values should be accepted; got: %v", err)
		}
	}
}

// asUsageError is a tiny errors.As shim that works without importing
// errors on every test-file change.
func asUsageError(err error, out **UsageError) bool {
	ue, ok := err.(*UsageError)
	if ok {
		*out = ue
	}
	return ok
}
