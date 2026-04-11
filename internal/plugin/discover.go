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

// Warnings holds non-fatal plugin discovery problems so callers can
// surface them to users instead of having broken plugins silently
// vanish. The previous code just `continue`d past every error.
var Warnings []string

func warn(format string, args ...any) {
	Warnings = append(Warnings, fmt.Sprintf(format, args...))
}

// Discover finds plugins in the given directories. Errors at any
// stage (directory read, manifest parse, sub-resource load) are
// captured into the package-level Warnings slice and surfaced via
// /doctor and /agents instead of silently dropped.
func Discover(dirs ...string) ([]*Plugin, error) {
	Warnings = Warnings[:0]
	var plugins []*Plugin
	for _, dir := range dirs {
		found, err := discoverInDir(dir)
		if err != nil {
			warn("plugin: scan %s failed: %v", dir, err)
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
		path := filepath.Join(dir, e.Name())
		p, err := Load(path)
		if err != nil {
			warn("plugin: load %s failed: %v", path, err)
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

	if cmds, err := loadCommands(pluginDir, manifest); err != nil {
		warn("plugin %s: commands: %v", pluginDir, err)
	} else {
		p.Commands = cmds
	}
	if ags, err := loadAgents(pluginDir, manifest); err != nil {
		warn("plugin %s: agents: %v", pluginDir, err)
	} else {
		p.Agents = ags
	}
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

func loadAgents(pluginDir string, m *Manifest) ([]*agent.Agent, error) {
	agentDir := filepath.Join(pluginDir, "agents")
	if m.Agents != "" {
		agentDir = filepath.Join(pluginDir, m.Agents)
	}
	return agent.Discover(agentDir)
}

func loadHooks(pluginDir string, m *Manifest, out map[string][]config.HookMatcherConfig) {
	hookFile := filepath.Join(pluginDir, "hooks", "hooks.json")
	if m.Hooks != "" {
		hookFile = filepath.Join(pluginDir, m.Hooks)
	}

	data, err := os.ReadFile(hookFile)
	if err != nil {
		// Missing hooks.json is normal — most plugins don't have one.
		// Other read errors (permission, etc.) are worth a warning.
		if !os.IsNotExist(err) {
			warn("plugin %s: hooks: %v", pluginDir, err)
		}
		return
	}

	var wrapper struct {
		Hooks map[string][]config.HookMatcherConfig `json:"hooks"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		warn("plugin %s: hooks json: %v", pluginDir, err)
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
