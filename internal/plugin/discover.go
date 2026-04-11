package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	return walkPluginDir(dir, 0)
}

// maxPluginDepth caps how deep we descend looking for plugin manifests.
// Claude Code's marketplace layout is cache/<owner>/<repo>/.claude-plugin/,
// so depth 3 covers it with margin. Without a cap, a symlink loop or a
// huge tree could spin forever.
const maxPluginDepth = 3

// walkPluginDir descends into dir looking for plugin manifests. A directory
// IS a plugin if it contains .altcode-plugin/plugin.json or
// .claude-plugin/plugin.json — in that case we add it and stop descending.
// Otherwise we recurse one level deeper, up to maxPluginDepth, to handle
// nested layouts like ~/.claude/plugins/cache/<owner>/<repo>/.
//
// Directories without a manifest are silently skipped — Claude Code's
// `cache`, `data`, and `marketplaces` siblings used to surface as warnings
// and frighten users into thinking their plugins were broken.
func walkPluginDir(dir string, depth int) ([]*Plugin, error) {
	if depth > maxPluginDepth {
		return nil, nil
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	// If THIS directory itself has a manifest, treat it as the plugin.
	if hasPluginManifest(dir) {
		p, err := Load(dir)
		if err != nil {
			warn("plugin: load %s failed: %v", dir, err)
			return nil, nil
		}
		return []*Plugin{p}, nil
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
		// Skip dotfiles — .git, .DS_Store-like dirs, .claude-plugin
		// itself shouldn't be re-walked.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		child := filepath.Join(dir, e.Name())
		nested, err := walkPluginDir(child, depth+1)
		if err != nil {
			warn("plugin: scan %s failed: %v", child, err)
			continue
		}
		plugins = append(plugins, nested...)
	}
	return plugins, nil
}

func hasPluginManifest(dir string) bool {
	for _, name := range []string{".altcode-plugin", ".claude-plugin"} {
		if _, err := os.Stat(filepath.Join(dir, name, "plugin.json")); err == nil {
			return true
		}
	}
	return false
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

// safeJoin joins a user-supplied subpath onto a trusted base directory
// and verifies the result stays inside base. Defends against malicious
// plugin.json fields like "commands":"../../../etc" that would otherwise
// escape the plugin directory.
func safeJoin(base, sub string) (string, error) {
	joined := filepath.Join(base, sub)
	cleanBase := filepath.Clean(base)
	cleanJoined := filepath.Clean(joined)
	rel, err := filepath.Rel(cleanBase, cleanJoined)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", fmt.Errorf("path %q escapes plugin directory", sub)
	}
	return cleanJoined, nil
}

func loadCommands(pluginDir string, m *Manifest) ([]*command.Command, error) {
	// Marketplace format: explicit list of relative file paths
	// (e.g. ["./commands/setup.md", "./commands/configure.md"]).
	if len(m.CommandFiles) > 0 {
		var out []*command.Command
		for _, rel := range m.CommandFiles {
			full, err := safeJoin(pluginDir, rel)
			if err != nil {
				return nil, fmt.Errorf("plugin command file %q: %w", rel, err)
			}
			cmd, err := command.ParseFile(full)
			if err != nil {
				return nil, fmt.Errorf("plugin command file %q: %w", rel, err)
			}
			out = append(out, cmd)
		}
		return out, nil
	}

	// Native format: directory of .md files.
	sub := "commands"
	if m.Commands != "" {
		sub = m.Commands
	}
	cmdDir, err := safeJoin(pluginDir, sub)
	if err != nil {
		return nil, fmt.Errorf("plugin commands path: %w", err)
	}
	return command.Discover(cmdDir)
}

func loadAgents(pluginDir string, m *Manifest) ([]*agent.Agent, error) {
	if len(m.AgentFiles) > 0 {
		var out []*agent.Agent
		for _, rel := range m.AgentFiles {
			full, err := safeJoin(pluginDir, rel)
			if err != nil {
				return nil, fmt.Errorf("plugin agent file %q: %w", rel, err)
			}
			ag, err := agent.ParseFile(full)
			if err != nil {
				return nil, fmt.Errorf("plugin agent file %q: %w", rel, err)
			}
			out = append(out, ag)
		}
		return out, nil
	}
	sub := "agents"
	if m.Agents != "" {
		sub = m.Agents
	}
	agentDir, err := safeJoin(pluginDir, sub)
	if err != nil {
		return nil, fmt.Errorf("plugin agents path: %w", err)
	}
	return agent.Discover(agentDir)
}

func loadHooks(pluginDir string, m *Manifest, out map[string][]config.HookMatcherConfig) {
	hookFile := filepath.Join(pluginDir, "hooks", "hooks.json")
	if m.Hooks != "" {
		// Validate the manifest-supplied path stays inside the plugin
		// directory. Without safeJoin, a malicious plugin could set
		// "hooks": "../../../etc/passwd" and we'd happily read it.
		joined, err := safeJoin(pluginDir, m.Hooks)
		if err != nil {
			warn("plugin %s: hooks path: %v", pluginDir, err)
			return
		}
		hookFile = joined
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
