// Package auth loads API credentials from Claude Code and Codex CLI configs.
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/altcode-ai/altcode/internal/config"
)

// LoadFromCLIs detects and loads credentials from installed Claude Code
// and Codex CLI configurations. Merges into the given config.
func LoadFromCLIs(cfg *config.Config) {
	loadClaudeCodeAuth(cfg)
	loadCodexAuth(cfg)
}

// loadClaudeCodeAuth reads ~/.claude/.credentials.json (Claude subscription)
func loadClaudeCodeAuth(cfg *config.Config) {
	if _, ok := cfg.Provider["anthropic"]; ok && cfg.Provider["anthropic"].APIKey != "" {
		return // already configured
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	path := filepath.Join(home, ".claude", ".credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var creds struct {
		ClaudeAiOauth struct {
			AccessToken      string `json:"accessToken"`
			SubscriptionType string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &creds) != nil || creds.ClaudeAiOauth.AccessToken == "" {
		return
	}

	cfg.Provider["anthropic"] = config.ProviderConfig{
		APIKey: creds.ClaudeAiOauth.AccessToken,
	}
}

// loadCodexAuth reads ~/.codex/auth.json + ~/.codex/config.toml (Codex subscription)
func loadCodexAuth(cfg *config.Config) {
	if _, ok := cfg.Provider["openai"]; ok && cfg.Provider["openai"].APIKey != "" {
		return // already configured
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	codexDir := filepath.Join(home, ".codex")

	// Read API key from auth.json
	authPath := filepath.Join(codexDir, "auth.json")
	authData, err := os.ReadFile(authPath)
	if err != nil {
		return
	}

	var auth map[string]any
	if json.Unmarshal(authData, &auth) != nil {
		return
	}

	apiKey, _ := auth["OPENAI_API_KEY"].(string)
	if apiKey == "" {
		return
	}

	// Read base URL from config.toml
	baseURL := parseCodexBaseURL(filepath.Join(codexDir, "config.toml"))

	cfg.Provider["openai"] = config.ProviderConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
	}

	// Set model from codex config if not already set
	if cfg.Model == config.DefaultModel {
		if model := parseCodexModel(filepath.Join(codexDir, "config.toml")); model != "" {
			cfg.Model = "openai/" + model
		}
	}
}

// parseCodexBaseURL extracts base_url from codex config.toml
func parseCodexBaseURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Simple TOML parsing for base_url
	for _, line := range splitLines(string(data)) {
		if key, val := parseTOMLKV(line); key == "base_url" {
			return val
		}
	}
	return ""
}

// parseCodexModel extracts model from codex config.toml
func parseCodexModel(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range splitLines(string(data)) {
		if key, val := parseTOMLKV(line); key == "model" {
			return val
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func parseTOMLKV(line string) (string, string) {
	for i := 0; i < len(line); i++ {
		if line[i] == '=' {
			key := trimSpace(line[:i])
			val := trimSpace(line[i+1:])
			// Remove quotes
			if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
				val = val[1 : len(val)-1]
			}
			return key, val
		}
	}
	return "", ""
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// CredentialSource returns a human-readable description of where credentials came from.
func CredentialSource(cfg *config.Config) string {
	home, _ := os.UserHomeDir()
	sources := ""

	if p, ok := cfg.Provider["anthropic"]; ok && p.APIKey != "" {
		if _, err := os.Stat(filepath.Join(home, ".claude", ".credentials.json")); err == nil {
			sources += "Claude subscription"
		} else if os.Getenv("ANTHROPIC_API_KEY") != "" {
			sources += "ANTHROPIC_API_KEY env"
		} else {
			sources += "config file"
		}
	}

	if p, ok := cfg.Provider["openai"]; ok && p.APIKey != "" {
		if sources != "" {
			sources += " + "
		}
		if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); err == nil {
			sources += "Codex subscription"
		} else if os.Getenv("OPENAI_API_KEY") != "" {
			sources += "OPENAI_API_KEY env"
		} else {
			sources += "config file"
		}
	}

	if sources == "" {
		return "no credentials"
	}
	return sources
}

// DefaultConfigDir returns the platform-appropriate config directory.
func DefaultConfigDir() string {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "altcode")
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "altcode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "altcode")
}
