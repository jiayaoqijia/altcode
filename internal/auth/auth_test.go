package auth_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/altcode-ai/altcode/internal/auth"
	"github.com/altcode-ai/altcode/internal/config"
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
