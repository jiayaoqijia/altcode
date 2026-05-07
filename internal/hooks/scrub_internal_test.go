package hooks

import (
	"strings"
	"testing"
)

// TestScrubSecrets_StripsCredentialEnvs verifies hooks don't inherit
// API keys / tokens from the parent. Codex round-H flagged that
// untrusted repo `.claude/settings.json` hooks were getting full env.
func TestScrubSecrets_StripsCredentialEnvs(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/tmp",
		"ANTHROPIC_API_KEY=sk-ant-xxx",
		"OPENAI_API_KEY=sk-openai-yyy",
		"DEEPSEEK_API_KEY=ds-zzz",
		"GITHUB_TOKEN=ghp_aaa",
		"AWS_SECRET_ACCESS_KEY=bbb",
		"SOME_PRIVATE_KEY=pem",
		"MY_SESSION_COOKIE=xyz",
		"USER_PASSWORD=pw",
		"AUTH_HEADER=Bearer xyz",
		"NPM_TOKEN=ccc",
		"GOOGLE_APPLICATION_CREDENTIALS=/tmp/gcp.json", // CREDENTIAL
	}
	out := scrubSecrets(env)

	// Should keep non-secret env.
	kept := strings.Join(out, "\n")
	for _, want := range []string{"PATH=/usr/bin", "HOME=/tmp"} {
		if !strings.Contains(kept, want) {
			t.Errorf("missing non-secret env: %s", want)
		}
	}

	// Should strip anything carrying a credential-ish name.
	secrets := []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY",
		"GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "SOME_PRIVATE_KEY",
		"MY_SESSION_COOKIE", "USER_PASSWORD", "AUTH_HEADER",
		"NPM_TOKEN", "GOOGLE_APPLICATION_CREDENTIALS",
	}
	for _, name := range secrets {
		if strings.Contains(kept, name+"=") {
			t.Errorf("secret %s leaked to hook env", name)
		}
	}
}

// TestScrubSecrets_PreservesNonEnvLines keeps malformed entries
// (no `=`) intact — filtering should be conservative.
func TestScrubSecrets_PreservesNonEnvLines(t *testing.T) {
	env := []string{"weird-line-no-equals", "PATH=/bin"}
	out := scrubSecrets(env)
	if len(out) != 2 {
		t.Errorf("want 2 entries preserved, got %d: %v", len(out), out)
	}
}
