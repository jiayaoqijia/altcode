package auth_test

import (
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
