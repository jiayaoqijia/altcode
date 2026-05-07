package tui

import (
	"strings"
	"testing"
)

func TestLooksLikeDiff(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"plain prose", "Hello, world.", false},
		{"unified header", "--- a/foo\n+++ b/foo\n", true},
		{"hunk header", "@@ -1,3 +1,3 @@\n", true},
		{"add+remove combo", "context\n-removed\n+added\n", true},
		{"only adds", "+lonely add\n", false}, // ambiguous; pure-add isn't diff
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeDiff(tc.in); got != tc.want {
				t.Errorf("looksLikeDiff(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseProvider(t *testing.T) {
	cases := map[string]string{
		"":                       "anthropic",
		"anthropic/claude-opus":  "anthropic",
		"openai/gpt-5.4":         "openai",
		"deepseek/deepseek-chat": "deepseek",
		"qwen/qwen-max":          "qwen",
		// No slash → return whole input (caller treats as provider name).
		"someprovider": "someprovider",
	}
	for in, want := range cases {
		if got := parseProvider(in); got != want {
			t.Errorf("parseProvider(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProviderHelpers(t *testing.T) {
	cases := []struct {
		provider          string
		setupPlaceholder  string
		loginLabel        string
		fmtLabel          string
	}{
		{"anthropic", "Paste your Anthropic API key and press Enter", "Claude Code", "Anthropic"},
		{"openai", "Paste your OpenAI API key and press Enter", "Codex", "OpenAI"},
		{"unknown", "Paste API key and press Enter", "your CLI", "Unknown"},
	}
	for _, tc := range cases {
		if got := providerSetupPlaceholder(tc.provider); got != tc.setupPlaceholder {
			t.Errorf("providerSetupPlaceholder(%q) = %q, want %q",
				tc.provider, got, tc.setupPlaceholder)
		}
		if got := providerLoginLabel(tc.provider); got != tc.loginLabel {
			t.Errorf("providerLoginLabel(%q) = %q, want %q",
				tc.provider, got, tc.loginLabel)
		}
		if got := providerLabel(tc.provider); got != tc.fmtLabel {
			t.Errorf("providerLabel(%q) = %q, want %q",
				tc.provider, got, tc.fmtLabel)
		}
	}
}

func TestNormalInputPlaceholder(t *testing.T) {
	cases := map[string]string{
		"":                          "Ask anything",
		"Welcome to Anthropic flow": "Anthropic API key",
		"Setup OpenAI access":       "OpenAI API key",
		"Some setup needed":         "start setup",
	}
	for prompt, expectedSubstr := range cases {
		got := normalInputPlaceholder(prompt)
		if !strings.Contains(got, expectedSubstr) {
			t.Errorf("normalInputPlaceholder(%q) = %q, missing %q",
				prompt, got, expectedSubstr)
		}
	}
}

func TestDisplayVersion(t *testing.T) {
	// displayVersion strips a leading 'v'/'V' and surrounding whitespace
	// so callers can always prepend a literal "v" without producing
	// "vv0.10.4" when the build is tagged with -X main.Version=v0.10.4.
	cases := map[string]string{
		"":         "dev",
		"   ":      "dev",
		"v":        "dev", // pure-prefix collapses to dev
		"v1.2.3":   "1.2.3",
		"V1.2.3":   "1.2.3",
		"vv1.2.3":  "1.2.3",
		"  pinned ": "pinned",
		"1.2.3":    "1.2.3",
	}
	for in, want := range cases {
		if got := displayVersion(in); got != want {
			t.Errorf("displayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFormatUSD covers the adaptive precision rules: 4-decimal under
// $0.01 (preserves half-cent), 3-decimal $0.01–$1 (e.g. $0.523), and
// 2-decimal at $1+ (natural reading for big costs). Round-5 finding:
// the HUD chip and inline turn summary used to disagree at the same
// cost level ($0.0052 vs $0.01); formatUSD makes them consistent.
func TestFormatUSD(t *testing.T) {
	cases := map[float64]string{
		0:       "$0.0000",
		0.0026:  "$0.0026", // sub-cent: 4-decimal preserves half-cents
		0.0099:  "$0.0099",
		0.01:    "$0.010",  // boundary: 3-decimal kicks in at exactly $0.01
		0.123:   "$0.123",
		0.999:   "$0.999",
		1.0:     "$1.00", // ≥ $1: 2-decimal
		1.2345:  "$1.23",
		12.34:   "$12.34",
		-0.0053: "-$0.0053", // negative: render magnitude with sign prefix
	}
	for in, want := range cases {
		if got := formatUSD(in); got != want {
			t.Errorf("formatUSD(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestLooksLikeAuthError(t *testing.T) {
	cases := []struct {
		msg, provider string
		want          bool
	}{
		{"openai: status 401 unauthorized", "openai", true},
		{"anthropic auth_error: invalid x-api-key", "anthropic", true},
		{"network: unable to connect", "openai", false},
		{"anthropic ratelimited 429", "anthropic", false},
		{"plain provider mention only", "anthropic", false},
	}
	for _, tc := range cases {
		got := looksLikeAuthError(tc.msg, tc.provider)
		if got != tc.want {
			t.Errorf("looksLikeAuthError(%q, %q) = %v, want %v",
				tc.msg, tc.provider, got, tc.want)
		}
	}
}

func TestMaxHelper(t *testing.T) {
	if got := max(3, 5); got != 5 {
		t.Errorf("max(3,5) = %d, want 5", got)
	}
	if got := max(7, 2); got != 7 {
		t.Errorf("max(7,2) = %d, want 7", got)
	}
	if got := max(-1, -3); got != -1 {
		t.Errorf("max(-1,-3) = %d, want -1", got)
	}
}
