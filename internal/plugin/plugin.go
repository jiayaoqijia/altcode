package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
)

// Manifest is the plugin.json metadata.
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Commands    string `json:"commands,omitempty"`
	Hooks       string `json:"hooks,omitempty"`
	Agents      string `json:"agents,omitempty"`
	MCPServers  string `json:"mcpServers,omitempty"`
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
