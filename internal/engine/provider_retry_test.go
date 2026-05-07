package engine

import (
	"os"
	"testing"
)

// TestProviderRetryBudget_DefaultZero verifies that test runs inherit
// the safe default — no retries, deterministic error propagation.
func TestProviderRetryBudget_DefaultZero(t *testing.T) {
	prev := os.Getenv("ALTCODE_PROVIDER_RETRIES")
	_ = os.Unsetenv("ALTCODE_PROVIDER_RETRIES")
	t.Cleanup(func() { _ = os.Setenv("ALTCODE_PROVIDER_RETRIES", prev) })

	if got := providerRetryBudget(); got != 0 {
		t.Errorf("default = %d, want 0", got)
	}
}

// TestProviderRetryBudget_HonorsEnv parses positive integers up to
// the safety cap.
func TestProviderRetryBudget_HonorsEnv(t *testing.T) {
	cases := map[string]int{
		"":         0,
		"0":        0,
		"3":        3,
		"5":        5,
		"-1":       0,    // invalid → 0
		"abc":      0,    // unparseable → 0
		"1000":     20,   // capped
	}
	for in, want := range cases {
		t.Setenv("ALTCODE_PROVIDER_RETRIES", in)
		if got := providerRetryBudget(); got != want {
			t.Errorf("env=%q: got %d, want %d", in, got, want)
		}
	}
}
