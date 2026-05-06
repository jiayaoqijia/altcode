package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jiayaoqijia/altcode/internal/agent"
	"github.com/jiayaoqijia/altcode/internal/command"
	"github.com/jiayaoqijia/altcode/internal/config"
)

// Manifest is the plugin.json metadata.
//
// `Commands`, `Hooks`, `Agents`, and `MCPServers` accept both shapes
// found in the wild:
//   - A directory string ("commands/") — altcode native format.
//   - An array of file paths (["./commands/setup.md", ...]) — Claude
//     Code marketplace format used by claude-hud and others.
//
// CommandFiles / AgentFiles hold the array form when present; the
// loader prefers explicit files over directory walking when both
// could apply.
type Manifest struct {
	Name         string   `json:"name"`
	Version      string   `json:"version,omitempty"`
	Description  string   `json:"description,omitempty"`
	Commands     string   `json:"-"`
	CommandFiles []string `json:"-"`
	Hooks        string   `json:"-"`
	HookFiles    []string `json:"-"`
	Agents       string   `json:"-"`
	AgentFiles   []string `json:"-"`
	MCPServers   string   `json:"-"`
}

// UnmarshalJSON accepts either a string or []string for the
// commands/hooks/agents/mcpServers fields. This makes altcode
// compatible with Claude Code's marketplace plugin format where
// these fields are arrays of explicit file paths.
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name        string          `json:"name"`
		Version     string          `json:"version,omitempty"`
		Description string          `json:"description,omitempty"`
		Commands    json.RawMessage `json:"commands,omitempty"`
		Hooks       json.RawMessage `json:"hooks,omitempty"`
		Agents      json.RawMessage `json:"agents,omitempty"`
		MCPServers  json.RawMessage `json:"mcpServers,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Name = raw.Name
	m.Version = raw.Version
	m.Description = raw.Description

	if err := decodeStringOrArray(raw.Commands, &m.Commands, &m.CommandFiles); err != nil {
		return fmt.Errorf("commands: %w", err)
	}
	if err := decodeStringOrArray(raw.Hooks, &m.Hooks, &m.HookFiles); err != nil {
		return fmt.Errorf("hooks: %w", err)
	}
	if err := decodeStringOrArray(raw.Agents, &m.Agents, &m.AgentFiles); err != nil {
		return fmt.Errorf("agents: %w", err)
	}
	// mcpServers is always a string in both formats today.
	if len(raw.MCPServers) > 0 {
		_ = json.Unmarshal(raw.MCPServers, &m.MCPServers)
	}
	return nil
}

// decodeStringOrArray sets *str if data is a JSON string, or *arr if
// data is a JSON array of strings. Empty/missing data is a no-op.
func decodeStringOrArray(data json.RawMessage, str *string, arr *[]string) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, str); err == nil {
		return nil
	}
	return json.Unmarshal(data, arr)
}

// Plugin is a loaded plugin with its resolved components.
type Plugin struct {
	Manifest Manifest
	Dir      string
	Commands []*command.Command
	Agents   []*agent.Agent
	Hooks    map[string][]config.HookMatcherConfig
}

// loadManifest reads plugin.json from .altcode-plugin/ or .claude-plugin/.
func loadManifest(pluginDir string) (*Manifest, error) {
	// Support both altcode and Claude Code plugin directories
	paths := []string{
		filepath.Join(pluginDir, ".altcode-plugin", "plugin.json"),
		filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	}
	return nil, fmt.Errorf("plugin.json not found in %s", pluginDir)
}
