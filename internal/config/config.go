package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const DefaultModel = "anthropic/claude-sonnet-4-20250514"

// Config holds the full application configuration.
type Config struct {
	Model            string                         `json:"model"`
	ContextWindow    int                            `json:"context_window,omitempty"`    // override context window size
	CompactThreshold int                            `json:"compact_threshold,omitempty"` // token count to trigger auto-compact
	Provider         map[string]ProviderConfig      `json:"provider"`
	Permission       []PermissionRule               `json:"permission"`
	MCP              map[string]MCPServerConfig     `json:"mcp"`
	Theme            string                         `json:"theme"`
	Agent            map[string]AgentConfig         `json:"agent"`
	Hooks            map[string][]HookMatcherConfig `json:"hooks"`
	Team             *TeamConfig                    `json:"team,omitempty"`
}

// TeamConfig defines a multi-AI orchestration team.
// Users configure which models play which roles.
type TeamConfig struct {
	Name    string                `json:"name,omitempty"`
	Models  map[string]TeamModel  `json:"models"`           // role → model config
	Default TeamDefaults          `json:"default,omitempty"` // fallback settings
}

// TeamModel defines a model assigned to a role.
type TeamModel struct {
	Model   string `json:"model"`             // e.g. "openai/gpt-5.4"
	APIKey  string `json:"apiKey,omitempty"`   // override key
	BaseURL string `json:"baseURL,omitempty"`  // override base URL
}

// TeamDefaults provides fallback configuration for team roles.
type TeamDefaults struct {
	Timeout int `json:"timeout,omitempty"` // seconds per model, default 60
}

// ProviderConfig holds API credentials for a model provider.
type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseURL"`
}

// PermissionRule controls which tools are allowed or denied.
type PermissionRule struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}

// MCPServerConfig describes an MCP server endpoint.
type MCPServerConfig struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Transport string            `json:"transport"`
}

// AgentConfig describes a named sub-agent's behaviour.
type AgentConfig struct {
	Model string   `json:"model"`
	Tools []string `json:"tools"`
}

// HookMatcherConfig pairs a tool matcher with hook entries.
type HookMatcherConfig struct {
	Matcher string            `json:"matcher"`
	Hooks   []HookEntryConfig `json:"hooks"`
}

// HookEntryConfig defines a single hook action.
type HookEntryConfig struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	return &Config{
		Model:      DefaultModel,
		Provider:   make(map[string]ProviderConfig),
		Permission: []PermissionRule{},
		MCP:        make(map[string]MCPServerConfig),
		Theme:      "default",
		Agent:      make(map[string]AgentConfig),
		Hooks:      make(map[string][]HookMatcherConfig),
	}
}

// LoadFile reads a JSONC config file, strips comments, expands env vars,
// and merges the result on top of the defaults. Validates simple
// constraints (positive numbers) so a typo doesn't silently fall
// back to a default value.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	stripped := stripJSONComments(string(data))
	expanded := ExpandEnv(stripped)

	cfg := Default()
	if err := json.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, err
	}

	// Reject obviously-bad values up front. Negative limits silently
	// turning into 'use default' is the wrong default — the user
	// should know they wrote 0 or -1.
	if cfg.ContextWindow < 0 {
		return nil, fmt.Errorf("config %s: context_window must be >= 0 (got %d)", path, cfg.ContextWindow)
	}
	if cfg.CompactThreshold < 0 {
		return nil, fmt.Errorf("config %s: compact_threshold must be >= 0 (got %d)", path, cfg.CompactThreshold)
	}
	return cfg, nil
}

// ExpandEnv replaces $VAR_NAME patterns with values from the environment.
// Unset variables warn to stderr (because the alternative is silently
// turning required-but-templated fields like API keys into empty
// strings, then loading 'successfully' with broken auth) and still
// expand to "" so the JSON remains valid.
func ExpandEnv(s string) string {
	re := regexp.MustCompile(`\$([A-Z_][A-Z0-9_]*)`)
	warned := map[string]bool{}
	return re.ReplaceAllStringFunc(s, func(match string) string {
		name := match[1:] // strip leading '$'
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		if !warned[name] {
			fmt.Fprintf(os.Stderr, "altcode: env var $%s referenced in config but not set; expanding to empty\n", name)
			warned[name] = true
		}
		return ""
	})
}

// stripJSONComments removes // line comments from a JSON string.
// This is intentionally naive: it does not handle comments inside string
// values, which is acceptable for typical config files.
func stripJSONComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := indexLineComment(line); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// indexLineComment returns the byte index of the first // comment that is
// not inside a double-quoted string, or -1 if none is found.
func indexLineComment(line string) int {
	inString := false
	for i := 0; i < len(line)-1; i++ {
		ch := line[i]
		if ch == '"' && (i == 0 || line[i-1] != '\\') {
			inString = !inString
		}
		if !inString && ch == '/' && line[i+1] == '/' {
			return i
		}
	}
	return -1
}
