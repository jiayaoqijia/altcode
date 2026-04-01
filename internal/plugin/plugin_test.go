package plugin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/plugin"
)

func setupPlugin(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, name)

	os.MkdirAll(filepath.Join(pluginDir, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"`+name+`","version":"1.0.0","description":"test plugin"}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "commands", "greet.md"),
		[]byte("---\ndescription: Say hello\n---\nSay hello to the user."), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"),
		[]byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo ok"}]}]}}`), 0o644)

	return dir
}

func TestLoadPlugin(t *testing.T) {
	dir := setupPlugin(t, "test-plugin")
	pluginDir := filepath.Join(dir, "test-plugin")

	p, err := plugin.Load(pluginDir)
	if err != nil {
		t.Fatal(err)
	}

	if p.Manifest.Name != "test-plugin" {
		t.Errorf("Name: %q", p.Manifest.Name)
	}
	if len(p.Commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(p.Commands))
	}
	if len(p.Hooks["PreToolUse"]) != 1 {
		t.Errorf("Expected 1 PreToolUse matcher, got %d", len(p.Hooks["PreToolUse"]))
	}
}

func TestDiscoverPlugins(t *testing.T) {
	dir := setupPlugin(t, "plugin-a")

	// Add second plugin
	os.MkdirAll(filepath.Join(dir, "plugin-b", ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugin-b", ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-b"}`), 0o644)

	plugins, err := plugin.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(plugins) != 2 {
		t.Fatalf("Expected 2 plugins, got %d", len(plugins))
	}
}

func TestDiscoverNonexistentDir(t *testing.T) {
	plugins, err := plugin.Discover("/nonexistent/plugins")
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 0 {
		t.Error("Expected empty for nonexistent dir")
	}
}

func TestDiscoverSkipsInvalidPlugin(t *testing.T) {
	dir := t.TempDir()
	// Directory without plugin.json
	os.MkdirAll(filepath.Join(dir, "not-a-plugin"), 0o755)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 0 {
		t.Error("Should skip directories without plugin.json")
	}
}

func TestMergeHooks(t *testing.T) {
	dir := setupPlugin(t, "merge-test")
	p, _ := plugin.Load(filepath.Join(dir, "merge-test"))

	cfg := config.Default()
	p.Merge(cfg)

	if len(cfg.Hooks["PreToolUse"]) != 1 {
		t.Errorf("Expected merged hook, got %d", len(cfg.Hooks["PreToolUse"]))
	}
}

func TestMergeMultiplePlugins(t *testing.T) {
	dir := setupPlugin(t, "plugin-x")

	// Second plugin with different hook
	os.MkdirAll(filepath.Join(dir, "plugin-y", ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugin-y", ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-y"}`), 0o644)
	os.MkdirAll(filepath.Join(dir, "plugin-y", "hooks"), 0o755)
	os.WriteFile(filepath.Join(dir, "plugin-y", "hooks", "hooks.json"),
		[]byte(`{"hooks":{"Stop":[{"matcher":"*","hooks":[{"type":"command","command":"echo stop"}]}]}}`), 0o644)

	plugins, _ := plugin.Discover(dir)
	cfg := config.Default()
	for _, p := range plugins {
		p.Merge(cfg)
	}

	if len(cfg.Hooks["PreToolUse"]) < 1 {
		t.Error("Should have PreToolUse from plugin-x")
	}
	if len(cfg.Hooks["Stop"]) < 1 {
		t.Error("Should have Stop from plugin-y")
	}
}
