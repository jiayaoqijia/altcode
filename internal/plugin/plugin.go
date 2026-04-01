package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
)

// Manifest is the plugin.json metadata.
type Manifest struct {
	Name        string `json:"name"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Commands    string `json:"commands,omitempty"`    // relative path to commands dir
	Hooks       string `json:"hooks,omitempty"`       // relative path to hooks.json
	Agents      string `json:"agents,omitempty"`      // relative path to agents dir
	MCPServers  string `json:"mcpServers,omitempty"`  // relative path to .mcp.json
}

// Plugin is a loaded plugin with its resolved components.
type Plugin struct {
	Manifest Manifest
	Dir      string
	Commands []*command.Command
	Hooks    map[string][]config.HookMatcherConfig
}

// loadManifest reads plugin.json from the given directory.
func loadManifest(pluginDir string) (*Manifest, error) {
	path := filepath.Join(pluginDir, ".altcode-plugin", "plugin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
