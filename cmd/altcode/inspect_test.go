package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/config"
)

// captureStdout temporarily replaces os.Stdout with a pipe and
// returns whatever the fn wrote. Used to test the print-* helpers
// without adding a Writer parameter to each function.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	if err := fn(); err != nil {
		os.Stdout = old
		_ = w.Close()
		t.Fatalf("fn: %v", err)
	}
	_ = w.Close()
	os.Stdout = old
	return <-done
}

// TestPrintConfig_RedactsProviderAPIKeys pins the core redaction
// behavior. Without it, users piping --print-config into bug
// reports would leak their credentials.
func TestPrintConfig_RedactsProviderAPIKeys(t *testing.T) {
	cfg := &config.Config{
		Model: "anthropic/claude-sonnet-4",
		Provider: map[string]config.ProviderConfig{
			"anthropic": {APIKey: "sk-ant-secret-key-should-not-appear"},
			"openai":    {APIKey: "sk-proj-real-one"},
		},
	}
	out := captureStdout(t, func() error { return printConfig(cfg) })
	if strings.Contains(out, "sk-ant-secret-key-should-not-appear") {
		t.Error("anthropic API key leaked in --print-config output")
	}
	if strings.Contains(out, "sk-proj-real-one") {
		t.Error("openai API key leaked in --print-config output")
	}
	if !strings.Contains(out, "<redacted>") {
		t.Error("expected <redacted> marker in output")
	}
}

// TestPrintConfig_RedactsMCPEnvSecrets pins the BLOCKER fix caught
// in Phase 10 review: MCPServerConfig.Env commonly carries
// ANTHROPIC_API_KEY / GITHUB_TOKEN / etc. and was previously
// serialized verbatim.
func TestPrintConfig_RedactsMCPEnvSecrets(t *testing.T) {
	cfg := &config.Config{
		Model: "anthropic/claude-sonnet-4",
		MCP: map[string]config.MCPServerConfig{
			"github": {
				Command: "github-mcp",
				Env: map[string]string{
					"GITHUB_TOKEN":      "ghp_real_token_never_leak",
					"ANTHROPIC_API_KEY": "sk-ant-mcp-secret",
				},
			},
		},
	}
	out := captureStdout(t, func() error { return printConfig(cfg) })

	if strings.Contains(out, "ghp_real_token_never_leak") {
		t.Error("GITHUB_TOKEN leaked in --print-config MCP output")
	}
	if strings.Contains(out, "sk-ant-mcp-secret") {
		t.Error("ANTHROPIC_API_KEY leaked in --print-config MCP output")
	}
	if !strings.Contains(out, "<redacted>") {
		t.Error("expected <redacted> marker in output")
	}

	// Verify the caller's live config is NOT mutated (redaction
	// must happen on a deep copy).
	if cfg.MCP["github"].Env["GITHUB_TOKEN"] != "ghp_real_token_never_leak" {
		t.Error("printConfig mutated caller's config — redaction leaked back")
	}
}

// TestPrintConfig_RedactsBaseURLWithCredentials pins the
// self-hosted-URL redaction (e.g. https://user:pass@host/api).
func TestPrintConfig_RedactsBaseURLWithCredentials(t *testing.T) {
	cfg := &config.Config{
		Provider: map[string]config.ProviderConfig{
			"selfhost": {BaseURL: "https://admin:hunter2@llm.internal/v1"},
		},
	}
	out := captureStdout(t, func() error { return printConfig(cfg) })
	if strings.Contains(out, "hunter2") {
		t.Error("embedded password leaked in --print-config")
	}
}

// TestPrintConfig_PlainBaseURLStaysVisible verifies non-credential
// BaseURLs aren't over-redacted.
func TestPrintConfig_PlainBaseURLStaysVisible(t *testing.T) {
	cfg := &config.Config{
		Provider: map[string]config.ProviderConfig{
			"openai": {BaseURL: "https://api.openai.com/v1"},
		},
	}
	out := captureStdout(t, func() error { return printConfig(cfg) })
	if !strings.Contains(out, "api.openai.com") {
		t.Error("plain BaseURL should not be redacted")
	}
}

// TestPrintConfig_RedactsTeamModelAPIKey pins the second secret
// leak Codex Phase 10 caught: team.models[*].apiKey was being
// serialized verbatim before the fix.
func TestPrintConfig_RedactsTeamModelAPIKey(t *testing.T) {
	cfg := &config.Config{
		Team: &config.TeamConfig{
			Models: map[string]config.TeamModel{
				"architect": {
					Model:  "anthropic/claude-opus",
					APIKey: "sk-team-role-secret-key",
				},
				"reviewer": {
					Model:   "openai/gpt-4",
					BaseURL: "https://admin:hunter2@llm.internal/v1",
				},
			},
		},
	}
	out := captureStdout(t, func() error { return printConfig(cfg) })
	if strings.Contains(out, "sk-team-role-secret-key") {
		t.Error("team model APIKey leaked")
	}
	if strings.Contains(out, "hunter2") {
		t.Error("team model BaseURL embedded creds leaked")
	}
	// Live config untouched
	if cfg.Team.Models["architect"].APIKey != "sk-team-role-secret-key" {
		t.Error("live config mutated — redaction leaked back")
	}
}

// TestPrintToolsList verifies the helper at least emits something
// that looks like the expected shape. Exact tool set changes over
// time so we just sanity-check the "N tools registered" footer.
func TestPrintToolsList(t *testing.T) {
	out := captureStdout(t, func() error { return printToolsList() })
	if !strings.Contains(out, "tools registered") {
		t.Errorf("expected 'tools registered' footer, got: %q", out)
	}
	// Common tools should always be present.
	for _, name := range []string{"read", "write", "bash"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected tool %q in output", name)
		}
	}
}

// TestPrintMCPServers_Empty covers the no-MCP case.
func TestPrintMCPServers_Empty(t *testing.T) {
	out := captureStdout(t, func() error {
		return printMCPServers(&config.Config{})
	})
	if !strings.Contains(out, "No MCP servers") {
		t.Errorf("expected empty-state message, got: %q", out)
	}
}

// TestPrintMCPServers_Listing verifies sort + formatting for two
// configured servers.
func TestPrintMCPServers_Listing(t *testing.T) {
	cfg := &config.Config{
		MCP: map[string]config.MCPServerConfig{
			"zebra":  {Command: "zebra-cmd"},
			"apple":  {Command: "apple-cmd", Args: []string{"--serve"}},
		},
	}
	out := captureStdout(t, func() error { return printMCPServers(cfg) })
	// Alphabetical sort: apple should appear before zebra
	ai := strings.Index(out, "apple")
	zi := strings.Index(out, "zebra")
	if ai < 0 || zi < 0 {
		t.Fatalf("expected both servers in output: %q", out)
	}
	if ai > zi {
		t.Error("MCP output not sorted alphabetically")
	}
	if !strings.Contains(out, "--serve") {
		t.Error("expected Args in output")
	}
}

// TestPrintConfig_ValidJSONEnvelope makes sure the output can be
// parsed back as JSON so scripts can pipe through jq.
func TestPrintConfig_ValidJSONEnvelope(t *testing.T) {
	cfg := &config.Config{Model: "anthropic/claude-sonnet-4"}
	out := captureStdout(t, func() error { return printConfig(cfg) })
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v\nout: %q", err, out)
	}
}

func TestTruncate_RuneSafeBoundary(t *testing.T) {
	// CJK and emoji characters are multibyte — byte-indexed truncation
	// can split a codepoint and produce invalid UTF-8. This test
	// guards the iter-6 rune-aware fix.
	cases := []struct {
		name     string
		in       string
		maxRunes int
		wantOK   func(out string) bool
	}{
		{
			name:     "short ascii pass-through",
			in:       "hello",
			maxRunes: 10,
			wantOK:   func(o string) bool { return o == "hello" },
		},
		{
			name:     "long ascii truncated",
			in:       strings.Repeat("a", 250),
			maxRunes: 200,
			wantOK: func(o string) bool {
				return strings.Contains(o, "...[50 more runes]")
			},
		},
		{
			name:     "CJK boundary",
			in:       strings.Repeat("你好", 150), // 300 runes, 900 bytes
			maxRunes: 200,
			wantOK: func(o string) bool {
				// Must be valid UTF-8 and flag the elision.
				return strings.Contains(o, "...[100 more runes]") &&
					utf8ValidStringCompat(o)
			},
		},
		{
			name:     "emoji boundary",
			in:       strings.Repeat("🚀", 250),
			maxRunes: 200,
			wantOK: func(o string) bool {
				return strings.Contains(o, "...[50 more runes]") &&
					utf8ValidStringCompat(o)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.maxRunes)
			if !tc.wantOK(got) {
				t.Errorf("truncate(%d runes, maxRunes=%d) → %q",
					len([]rune(tc.in)), tc.maxRunes, got)
			}
		})
	}
}

// utf8ValidStringCompat avoids pulling in the unicode/utf8 import for
// a single call site.
func utf8ValidStringCompat(s string) bool {
	for _, r := range s {
		if r == 0xFFFD {
			return false
		}
	}
	return true
}
