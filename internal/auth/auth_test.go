package auth_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/auth"
	"github.com/jiayaoqijia/altcode/internal/config"
)

func TestLoadClaudeCodeAuth(t *testing.T) {
	home, _ := os.UserHomeDir()
	credPath := filepath.Join(home, ".claude", ".credentials.json")
	if _, err := os.Stat(credPath); os.IsNotExist(err) {
		t.Skip("No Claude Code credentials found")
	}

	cfg := config.Default()
	auth.LoadFromCLIs(cfg)

	if p, ok := cfg.Provider["anthropic"]; !ok || p.APIKey == "" {
		t.Error("Should load Anthropic key from Claude Code credentials")
	} else {
		t.Logf("Loaded Anthropic key: %s...%s", p.APIKey[:10], p.APIKey[len(p.APIKey)-4:])
	}
}

func TestLoadCodexAuth(t *testing.T) {
	home, _ := os.UserHomeDir()
	authPath := filepath.Join(home, ".codex", "auth.json")
	if _, err := os.Stat(authPath); os.IsNotExist(err) {
		t.Skip("No Codex credentials found")
	}

	cfg := config.Default()
	auth.LoadFromCLIs(cfg)

	if p, ok := cfg.Provider["openai"]; !ok || p.APIKey == "" {
		t.Error("Should load OpenAI key from Codex auth")
	} else {
		t.Logf("Loaded OpenAI key: %s...%s", p.APIKey[:10], p.APIKey[len(p.APIKey)-4:])
		if p.BaseURL != "" {
			t.Logf("Loaded base URL: %s", p.BaseURL)
		}
	}

	if cfg.Model != config.DefaultModel {
		t.Logf("Loaded model from Codex: %s", cfg.Model)
	}
}

// TestLoadCodexAuth_SectionedBaseURL is the regression test for the
// Phase 13 bug hunt finding: Codex config.toml puts base_url inside
// [model_providers.OpenAI], not at the top level. The previous
// parseCodexBaseURL only read the top level and silently dropped
// the custom endpoint, so altcode users with a Codex proxy had
// their key loaded but their endpoint reset to api.openai.com.
func TestLoadCodexAuth_SectionedBaseURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	codexDir := filepath.Join(dir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Modern Codex auth.json: apikey mode.
	authJSON := []byte(`{
		"auth_mode": "apikey",
		"OPENAI_API_KEY": "sk-test-key-12345"
	}`)
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), authJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	// Modern Codex config.toml: base_url inside a section header.
	tomlContent := `model_provider = "OpenAI"
model = "gpt-5.4"

[model_providers.OpenAI]
name = "OpenAI"
base_url = "https://proxy.example.com"
wire_api = "responses"
`
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	auth.LoadFromCLIs(cfg)

	p, ok := cfg.Provider["openai"]
	if !ok || p.APIKey == "" {
		t.Fatal("expected openai provider key from Codex auth.json")
	}
	if p.BaseURL != "https://proxy.example.com" {
		t.Errorf("BaseURL = %q, want https://proxy.example.com "+
			"(regression: section-qualified base_url should be loaded)",
			p.BaseURL)
	}
}

// TestLoadCodexAuth_LegacyTopLevelBaseURL verifies the legacy
// fallback for older Codex configs that put base_url at the top
// level still works.
func TestLoadCodexAuth_LegacyTopLevelBaseURL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	codexDir := filepath.Join(dir, ".codex")
	_ = os.MkdirAll(codexDir, 0o755)
	authJSON := []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"sk-legacy"}`)
	_ = os.WriteFile(filepath.Join(codexDir, "auth.json"), authJSON, 0o600)

	tomlContent := `base_url = "https://legacy.example.com"
model = "gpt-4"
`
	_ = os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(tomlContent), 0o644)

	cfg := config.Default()
	auth.LoadFromCLIs(cfg)

	if cfg.Provider["openai"].BaseURL != "https://legacy.example.com" {
		t.Errorf("legacy top-level base_url not loaded: got %q",
			cfg.Provider["openai"].BaseURL)
	}
}

func TestLoadBothProviders(t *testing.T) {
	home, _ := os.UserHomeDir()
	hasClaude := false
	hasCodex := false
	if _, err := os.Stat(filepath.Join(home, ".claude", ".credentials.json")); err == nil {
		hasClaude = true
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); err == nil {
		hasCodex = true
	}
	if !hasClaude && !hasCodex {
		t.Skip("No CLI credentials found")
	}

	cfg := config.Default()
	auth.LoadFromCLIs(cfg)

	source := auth.CredentialSource(cfg)
	t.Logf("Credential source: %s", source)

	if hasClaude {
		if _, ok := cfg.Provider["anthropic"]; !ok {
			t.Error("Should have anthropic provider")
		}
	}
	if hasCodex {
		if _, ok := cfg.Provider["openai"]; !ok {
			t.Error("Should have openai provider")
		}
	}
}

func TestNoCredsDoesNotPanic(t *testing.T) {
	// Even without credentials, LoadFromCLIs should not panic
	cfg := config.Default()
	// Pre-set a key to ensure it's not overwritten
	cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "existing"}
	auth.LoadFromCLIs(cfg)

	if cfg.Provider["anthropic"].APIKey != "existing" {
		t.Error("Should not overwrite existing key")
	}
}

func TestCredentialSource(t *testing.T) {
	cfg := config.Default()
	if auth.CredentialSource(cfg) != "no credentials" {
		t.Error("Empty config should say no credentials")
	}
}

func TestMissingCredentialPromptAnthropic(t *testing.T) {
	cfg := config.Default()
	cfg.Model = "anthropic/claude-sonnet-4-20250514"

	prompt := auth.MissingCredentialPrompt(cfg)
	if prompt == "" {
		t.Fatal("expected missing credential prompt for anthropic model")
	}
}

func TestMissingCredentialPromptOpenAI(t *testing.T) {
	cfg := config.Default()
	cfg.Model = "openai/gpt-5"

	prompt := auth.MissingCredentialPrompt(cfg)
	if prompt == "" {
		t.Fatal("expected missing credential prompt for openai model")
	}
}

func TestMissingCredentialPromptLocalModel(t *testing.T) {
	cfg := config.Default()
	cfg.Model = "ollama/llama3"

	prompt := auth.MissingCredentialPrompt(cfg)
	if prompt != "" {
		t.Fatalf("expected no prompt for local model, got %q", prompt)
	}
}

func TestSaveProviderAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := auth.SaveProviderAPIKey("openai", "test-key")
	if err != nil {
		t.Fatalf("SaveProviderAPIKey returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if got := cfg.Provider["openai"].APIKey; got != "test-key" {
		t.Fatalf("expected saved openai key, got %q", got)
	}

	wantPath := filepath.Join(home, ".altcode", "config.json")
	if path != wantPath {
		t.Fatalf("expected hidden home config path %q, got %q", wantPath, path)
	}
}
