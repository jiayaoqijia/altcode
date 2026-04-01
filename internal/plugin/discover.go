package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
)

// Discover finds plugins in the given directories.
// Each subdirectory containing .altcode-plugin/plugin.json is a plugin.
func Discover(dirs ...string) ([]*Plugin, error) {
	var plugins []*Plugin
	for _, dir := range dirs {
		found, err := discoverInDir(dir)
		if err != nil {
			continue
		}
		plugins = append(plugins, found...)
	}
	return plugins, nil
}

func discoverInDir(dir string) ([]*Plugin, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var plugins []*Plugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pluginDir := filepath.Join(dir, e.Name())
		p, err := Load(pluginDir)
		if err != nil {
			continue
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

// Load reads a single plugin from a directory.
func Load(pluginDir string) (*Plugin, error) {
	manifest, err := loadManifest(pluginDir)
	if err != nil {
		return nil, err
	}

	p := &Plugin{
		Manifest: *manifest,
		Dir:      pluginDir,
		Hooks:    make(map[string][]config.HookMatcherConfig),
	}

	p.Commands, _ = loadCommands(pluginDir, manifest)
	loadHooks(pluginDir, manifest, p.Hooks)

	return p, nil
}

func loadCommands(pluginDir string, m *Manifest) ([]*command.Command, error) {
	cmdDir := filepath.Join(pluginDir, "commands")
	if m.Commands != "" {
		cmdDir = filepath.Join(pluginDir, m.Commands)
	}
	return command.Discover(cmdDir)
}

func loadHooks(pluginDir string, m *Manifest, out map[string][]config.HookMatcherConfig) {
	hookFile := filepath.Join(pluginDir, "hooks", "hooks.json")
	if m.Hooks != "" {
		hookFile = filepath.Join(pluginDir, m.Hooks)
	}

	data, err := os.ReadFile(hookFile)
	if err != nil {
		return
	}

	// Plugin hooks.json has wrapper: {"hooks": {...}}
	var wrapper struct {
		Hooks map[string][]config.HookMatcherConfig `json:"hooks"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return
	}
	for k, v := range wrapper.Hooks {
		out[k] = append(out[k], v...)
	}
}

// Merge integrates a plugin's components into the global config.
func (p *Plugin) Merge(cfg *config.Config) {
	for event, matchers := range p.Hooks {
		cfg.Hooks[event] = append(cfg.Hooks[event], matchers...)
	}
}
