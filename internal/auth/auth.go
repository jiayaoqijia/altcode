// Package auth loads API credentials from Claude Code and Codex CLI configs.
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/config"
)

// LoadFromCLIs detects and loads credentials from Claude Code, Codex CLI,
// and altcode's own OAuth login file. Merges into the given config.
func LoadFromCLIs(cfg *config.Config) {
	loadClaudeCodeAuth(cfg)
	loadCodexAuth(cfg)
	loadAltcodeAuth(cfg)
}

// loadAltcodeAuth reads ~/.altcode/auth.json written by `altcode login`.
// If the cached access_token is stale AND a refresh_token exists, it
// attempts a best-effort refresh and writes the new token back.
// Codex round-J caught that the previous version ignored refresh_token
// entirely, so sessions silently failed with 401 once the ~30-day
// access token expired — until the user manually re-ran `altcode login`.
func loadAltcodeAuth(cfg *config.Config) {
	if p, ok := cfg.Provider["openai"]; ok && p.APIKey != "" {
		return // already configured
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".altcode", "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var a struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
		OpenAIKey   string     `json:"OPENAI_API_KEY"`
		LastRefresh *time.Time `json:"last_refresh"`
	}
	if json.Unmarshal(data, &a) != nil {
		return
	}
	token := a.Tokens.AccessToken
	if token == "" {
		token = a.OpenAIKey
	}
	if token == "" {
		return
	}

	// If the cached access token looks stale (>25 days since the last
	// successful refresh; ChatGPT access tokens live ~30 days) and we
	// have a refresh token, try a best-effort refresh before handing
	// the (possibly-expired) token to the provider. A refresh failure
	// falls through to the old behaviour — the user gets a 401 from
	// the provider but at least we tried.
	if a.Tokens.RefreshToken != "" && tokenIsStale(a.LastRefresh) {
		if refreshed := refreshAltcodeToken(path, a.Tokens.RefreshToken); refreshed != "" {
			token = refreshed
		}
	}

	cfg.Provider["openai"] = config.ProviderConfig{
		APIKey:  token,
		BaseURL: "https://chatgpt.com/backend-api/codex",
	}
}

// tokenIsStale reports whether a cached access token is old enough
// that we should try refreshing it. Threshold is 25 days — chosen to
// leave a comfortable margin before the typical 30-day expiry.
func tokenIsStale(lastRefresh *time.Time) bool {
	if lastRefresh == nil {
		return true
	}
	return time.Since(*lastRefresh) > 25*24*time.Hour
}

// refreshAltcodeTokenFn is the live token-refresh implementation.
// It's injected as a var so auth.go doesn't have to import the oauth
// package directly (and can be stubbed in tests). The oauth package
// registers itself via oauth.init() by calling RegisterRefresh.
// Returns "" on any failure — the caller falls through to the cached
// (possibly expired) token so behaviour is never worse than pre-fix.
var refreshAltcodeTokenFn func(path, refreshToken string) string

// refreshAltcodeToken is a tiny dispatcher so loadAltcodeAuth can call
// the refresh hook through a stable name.
func refreshAltcodeToken(path, refreshToken string) string {
	if refreshAltcodeTokenFn == nil {
		return ""
	}
	return refreshAltcodeTokenFn(path, refreshToken)
}

// RegisterRefresh installs the live token-refresh implementation.
// Called from internal/oauth.init().
func RegisterRefresh(fn func(path, refreshToken string) string) {
	refreshAltcodeTokenFn = fn
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
			ExpiresAt        int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &creds) != nil || creds.ClaudeAiOauth.AccessToken == "" {
		return
	}
	// Skip expired tokens — without this, the cached access token gets
	// happily loaded into the config and downstream API calls fail
	// with confusing 401s instead of triggering the user to re-auth.
	// expiresAt is unix milliseconds.
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		if time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt).Before(time.Now()) {
			fmt.Fprintln(os.Stderr, "altcode: Claude Code subscription token expired — run 'claude /login' to refresh")
			return
		}
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

// parseCodexBaseURL extracts base_url from Codex's config.toml.
// Checks (in order):
//  1. The `[model_providers.<ModelProvider>]` section, where
//     ModelProvider comes from the top-level `model_provider`
//     field. This is how the current (2026+) Codex CLI writes
//     custom provider endpoints.
//  2. The top-level `base_url` key (legacy layout).
//
// Skips any value inside unrelated sections. Before the Phase 13
// bug hunt caught this, altcode only looked at the top level and
// silently ignored the current `[model_providers.OpenAI]` layout,
// so Codex users with a custom base_url (e.g. a self-hosted
// Codex proxy) had their key loaded but their endpoint silently
// reset to api.openai.com.
func parseCodexBaseURL(path string) string {
	// Preferred: look up via the active model_provider.
	providerName := parseCodexTOMLTop(path, "model_provider")
	if providerName != "" {
		section := "[model_providers." + providerName + "]"
		if v := parseCodexTOMLSection(path, section, "base_url"); v != "" {
			return v
		}
	}
	// Legacy fallback: top-level base_url (pre-2026 Codex configs).
	return parseCodexTOMLTop(path, "base_url")
}

// parseCodexModel extracts the top-level model from codex config.toml.
// Same section-aware behavior as parseCodexBaseURL — a [profile.x]
// model used to silently win.
func parseCodexModel(path string) string {
	return parseCodexTOMLTop(path, "model")
}

// parseCodexTOMLSection returns the value of `key` within `section`
// (the `[header]` line including brackets). Returns empty string
// on any read or parse failure so callers can fall through to
// alternative sources.
func parseCodexTOMLSection(path, section, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	inTarget := false
	for _, raw := range splitLines(string(data)) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inTarget = (line == section)
			continue
		}
		if !inTarget {
			continue
		}
		if k, v := parseTOMLKV(line); k == key {
			return v
		}
	}
	return ""
}

// parseCodexTOMLTop returns the value of `key` if it appears at the
// top level of a TOML file, ignoring any value inside a [section]
// header. The previous parser walked every line and returned the
// first match regardless of nesting, so a [profile.staging] block
// could silently override the active model or base URL.
func parseCodexTOMLTop(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	currentSection := ""
	for _, raw := range splitLines(string(data)) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = line
			continue
		}
		if currentSection != "" {
			continue
		}
		if k, v := parseTOMLKV(line); k == key {
			return v
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

// MissingCredentialPrompt returns a startup prompt when the active model
// requires credentials that are not configured yet.
func MissingCredentialPrompt(cfg *config.Config) string {
	switch currentProvider(cfg.Model) {
	case "anthropic":
		if hasProviderKey(cfg, "anthropic") {
			return ""
		}
		return "No Anthropic credentials detected for the current model. Sign in to Claude Code, set ANTHROPIC_API_KEY, add provider.anthropic.apiKey in config, or switch to a local model like ollama/... or lmstudio/.... Restart altcode after updating credentials."
	case "openai":
		if hasProviderKey(cfg, "openai") {
			return ""
		}
		return "No OpenAI credentials detected for the current model. Sign in to Codex, set OPENAI_API_KEY, add provider.openai.apiKey in config, or switch to a local model like ollama/... or lmstudio/.... Restart altcode after updating credentials."
	default:
		return ""
	}
}

// UserConfigPath returns the default user config path used by altcode.
func UserConfigPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".altcode", "config.json")
}

// LegacyUserConfigPaths returns older user config paths that altcode still reads.
func LegacyUserConfigPaths() []string {
	home := userHomeDir()

	paths := []string{
		filepath.Join(home, "Library", "Application Support", "altcode", "config.json"),
	}

	legacyXDG := filepath.Join(home, ".config", "altcode", "config.json")
	if legacyXDG != paths[0] {
		paths = append(paths, legacyXDG)
	}

	return paths
}

func userHomeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return home
}

// SaveProviderAPIKey persists a provider API key into the user config file.
func SaveProviderAPIKey(providerName, apiKey string) (string, error) {
	path := UserConfigPath()

	// Load only the on-disk config — NOT the merged runtime config which
	// may contain auto-detected credentials from Claude Code / Codex CLI
	// credential stores. Writing those foreign keys to altcode's own
	// config file would be a credential-leak bug.
	cfg := config.Default()
	if _, err := os.Stat(path); err == nil {
		existing, err := config.LoadFile(path)
		if err != nil {
			return "", err
		}
		cfg = existing
	} else if !os.IsNotExist(err) {
		return "", err
	}

	// Only update the specific provider being saved. Any other
	// provider keys in cfg came from the on-disk file (user-configured)
	// so they are safe to persist.
	pcfg := cfg.Provider[providerName]
	pcfg.APIKey = apiKey
	cfg.Provider[providerName] = pcfg

	// Strip provider entries whose APIKey is empty — they were never
	// user-configured and would just be noise in the config file.
	for name, p := range cfg.Provider {
		if name != providerName && p.APIKey == "" && p.BaseURL == "" {
			delete(cfg.Provider, name)
		}
	}

	// Directory holds config.json with provider API keys — keep it
	// owner-only (0700) so the file's 0600 permissions are not
	// undermined by a world-readable parent that leaks metadata and
	// sibling files. altcode-TUI round-J adversarial review.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func hasProviderKey(cfg *config.Config, name string) bool {
	if p, ok := cfg.Provider[name]; ok && p.APIKey != "" {
		return true
	}
	// Fall back to the conventional environment variable for that
	// provider — without this, users with only ANTHROPIC_API_KEY /
	// OPENAI_API_KEY in env got a "no credentials detected" prompt
	// at startup even though CredentialSource happily picks them up
	// at request time.
	if env := providerEnvVar(name); env != "" && os.Getenv(env) != "" {
		return true
	}
	return false
}

// providerEnvVar maps a provider name to the canonical env var that
// holds its API key. Returns "" for providers without a conventional
// env name (subscription-only auth like Claude Code OAuth).
func providerEnvVar(name string) string {
	switch name {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "zhipu", "glm":
		return "ZHIPU_API_KEY"
	case "moonshot", "kimi":
		return "MOONSHOT_API_KEY"
	case "minimax":
		return "MINIMAX_API_KEY"
	case "qwen":
		return "DASHSCOPE_API_KEY"
	}
	return ""
}

func currentProvider(model string) string {
	for i := 0; i < len(model); i++ {
		if model[i] == '/' {
			return model[:i]
		}
	}
	if model == "" {
		return "anthropic"
	}
	return model
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
