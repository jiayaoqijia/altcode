package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
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

// expandEnvRE matches both uppercase and lowercase env var names. POSIX
// convention uses uppercase, but plenty of users have lowercase vars
// for one-off keys (e.g. `$openai_key`) and the previous uppercase-only
// regex silently dropped them with no warning.
var expandEnvRE = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)

// expandEnvWarned tracks which env-var names we've already warned about
// across the entire process startup. Without this, every config file
// that references the same unset variable produces a duplicate
// warning — typical altcode startup loads ~/.altcode/config.json AND
// $PWD/.altcode/config.json AND any legacy paths, so a single missing
// $OPENROUTER warns 2-3 times. The map is process-global, mutex-
// protected because parallel subagent spawning can cross-call
// LoadFile concurrently.
var (
	expandEnvWarnedMu sync.Mutex
	expandEnvWarned   = map[string]bool{}
)

// ExpandEnv replaces $VAR_NAME patterns with values from the environment.
// Unset variables warn to stderr ONCE per process (because the
// alternative is silently turning required-but-templated fields like
// API keys into empty strings, then loading 'successfully' with
// broken auth) and still expand to "" so the JSON remains valid.
//
// Substituted values are JSON-escaped before insertion. Without
// escaping, an env var containing `"` or `\` (legitimate password
// content) would inject syntactically broken JSON and json.Unmarshal
// would fail with a confusing parse error far from the offending
// variable. The substitution happens on raw JSON source, so the
// inserted string must be safe for the surrounding `"..."` context.
func ExpandEnv(s string) string {
	return expandEnvRE.ReplaceAllStringFunc(s, func(match string) string {
		name := match[1:] // strip leading '$'
		if val, ok := os.LookupEnv(name); ok {
			return jsonStringContent(val)
		}
		expandEnvWarnedMu.Lock()
		first := !expandEnvWarned[name]
		expandEnvWarned[name] = true
		expandEnvWarnedMu.Unlock()
		if first {
			fmt.Fprintf(os.Stderr,
				"altcode: $%s not set — leaving config field empty. "+
					"Set the env var (e.g. `export %s=...`) or replace "+
					"\"$%s\" with a literal value in your altcode config.\n",
				name, name, name)
		}
		return ""
	})
}

// resetExpandEnvWarnedForTest clears the package-level dedup map so
// tests that exercise the warning path don't pollute each other.
// Not exported — tests in this package call it directly.
func resetExpandEnvWarnedForTest() {
	expandEnvWarnedMu.Lock()
	expandEnvWarned = map[string]bool{}
	expandEnvWarnedMu.Unlock()
}

// jsonStringContent returns s ready to be embedded inside a JSON
// string literal — escaping `"`, `\`, and control characters. Used
// by ExpandEnv so an env var like `pass"word` doesn't break the
// surrounding JSON when substituted into `"apiKey": "$VAR"`.
func jsonStringContent(s string) string {
	b, err := json.Marshal(s)
	if err != nil || len(b) < 2 {
		return s
	}
	return string(b[1 : len(b)-1])
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
//
// Counts CONSECUTIVE preceding backslashes to detect escaped quotes
// correctly. The previous "look at line[i-1]" check failed for any
// double-escaped backslash followed by a quote, so a Windows path
// like `"path": "C:\\Users\\me"  // comment` made the parser think
// the closing quote was still inside a string and never stripped
// the comment, producing a confusing JSON parse error.
func indexLineComment(line string) int {
	inString := false
	for i := 0; i < len(line)-1; i++ {
		ch := line[i]
		if ch == '"' {
			// Count consecutive backslashes to the left. Even count
			// (including zero) means the quote is unescaped.
			bs := 0
			for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
				bs++
			}
			if bs%2 == 0 {
				inString = !inString
			}
		}
		if !inString && ch == '/' && line[i+1] == '/' {
			return i
		}
	}
	return -1
}
