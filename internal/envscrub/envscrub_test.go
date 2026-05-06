package envscrub

import (
	"strings"
	"testing"
)

// TestScrub_DropsCredentialPatterns verifies that the credential
// substring list catches the common API-key / token / secret naming
// conventions used by altcode's provider matrix.
func TestScrub_DropsCredentialPatterns(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"HOME=/tmp",
		"ANTHROPIC_API_KEY=sk-ant-xxx",
		"OPENAI_API_KEY=sk-yyy",
		"DEEPSEEK_API_KEY=ds",
		"GITHUB_TOKEN=ghp",
		"AWS_SECRET_ACCESS_KEY=zzz",
		"USER_PASSWORD=pw",
		"SOME_PRIVATE_KEY=pem",
		"AUTH_HEADER=Bearer x",
		"MY_SESSION_ID=abc",
		"GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp.json",
	}
	out := strings.Join(Scrub(in), "\n")
	for _, want := range []string{"PATH=/usr/bin", "HOME=/tmp"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing non-secret env: %s", want)
		}
	}
	for _, drop := range []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY",
		"GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "USER_PASSWORD",
		"PRIVATE_KEY", "AUTH_HEADER", "SESSION", "CREDENTIALS",
	} {
		if strings.Contains(out, drop+"=") {
			t.Errorf("secret leaked: %s", drop)
		}
	}
}

// TestScrub_DropsInternalPrefixes guards the Codex round-R parity-
// with-claude-code-2.1.128 finding: OTEL_* / CLAUDE_* / ALTCODE_* /
// ANTHROPIC_LOG / ANTHROPIC_DEBUG must NOT leak to children, even
// though they don't match the credential substring list.
func TestScrub_DropsInternalPrefixes(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://otel:4318",
		"OTEL_SERVICE_NAME=altcode",
		"CLAUDE_CODE_DEBUG=1",
		"CLAUDE_PROJECT_DIR=/tmp",
		"ALTCODE_HOOK_DEPTH=0",
		"ALTCODE_PLAN_MODEL=foo",
		"ANTHROPIC_LOG=debug",
		"ANTHROPIC_DEBUG=1",
	}
	out := strings.Join(Scrub(in), "\n")
	if !strings.Contains(out, "PATH=/usr/bin") {
		t.Error("PATH should pass through")
	}
	for _, drop := range []string{
		"OTEL_", "CLAUDE_", "ALTCODE_",
		"ANTHROPIC_LOG=", "ANTHROPIC_DEBUG=",
	} {
		if strings.Contains(out, drop) {
			t.Errorf("internal-prefix env leaked: %s", drop)
		}
	}
}

// TestScrub_PreservesMalformedEntries keeps env entries without `=`
// intact (rare but possible — Go's os.Environ never yields these,
// but third-party env-builders sometimes do).
func TestScrub_PreservesMalformedEntries(t *testing.T) {
	in := []string{"weird-no-equals", "PATH=/bin"}
	out := Scrub(in)
	if len(out) != 2 {
		t.Errorf("want 2 entries preserved, got %d: %v", len(out), out)
	}
}
