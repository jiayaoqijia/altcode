package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDefaults verifies that Default() returns sensible zero values.
func TestLoadDefaults(t *testing.T) {
	cfg := Default()

	if cfg.Model != DefaultModel {
		t.Errorf("Model = %q, want %q", cfg.Model, DefaultModel)
	}
	if cfg.Provider == nil {
		t.Error("Provider map should be non-nil")
	}
	if cfg.MCP == nil {
		t.Error("MCP map should be non-nil")
	}
	if cfg.Agent == nil {
		t.Error("Agent map should be non-nil")
	}
	if cfg.Hooks == nil {
		t.Error("Hooks map should be non-nil")
	}
	if cfg.Permission == nil {
		t.Error("Permission slice should be non-nil")
	}
	if cfg.Theme != "default" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "default")
	}
}

// TestLoadFromFile verifies that LoadFile parses a valid JSONC config.
func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.jsonc")

	content := `{
  // model selection
  "model": "openai/gpt-4o",
  "theme": "dark",
  "provider": {
    "openai": { "apiKey": "test-key" }
  }
}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}

	if cfg.Model != "openai/gpt-4o" {
		t.Errorf("Model = %q, want %q", cfg.Model, "openai/gpt-4o")
	}
	if cfg.Theme != "dark" {
		t.Errorf("Theme = %q, want %q", cfg.Theme, "dark")
	}
	p, ok := cfg.Provider["openai"]
	if !ok {
		t.Fatal("provider 'openai' not found")
	}
	if p.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", p.APIKey, "test-key")
	}
}

// TestEnvVarExpansion verifies that ExpandEnv replaces $VAR patterns.
func TestEnvVarExpansion(t *testing.T) {
	t.Setenv("MY_API_KEY", "secret-value")

	input := `{"provider": {"test": {"apiKey": "$MY_API_KEY"}}}`
	got := ExpandEnv(input)

	want := `{"provider": {"test": {"apiKey": "secret-value"}}}`
	if got != want {
		t.Errorf("ExpandEnv result:\n got  %q\n want %q", got, want)
	}

	// Unset variable should expand to empty string.
	os.Unsetenv("UNSET_VAR_XYZ")
	result := ExpandEnv("value=$UNSET_VAR_XYZ end")
	if result != "value= end" {
		t.Errorf("unset var: got %q, want %q", result, "value= end")
	}
}

// TestDetectProjectRoot verifies that DetectProjectRoot walks up to .git.
func TestDetectProjectRoot(t *testing.T) {
	base := t.TempDir()

	// Create a fake git root.
	gitRoot := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(gitRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a nested subdirectory.
	nested := filepath.Join(gitRoot, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got := DetectProjectRoot(nested)
	if got != gitRoot {
		t.Errorf("DetectProjectRoot = %q, want %q", got, gitRoot)
	}
}

// TestDetectProjectRootFallback verifies that startDir is returned when no .git exists.
func TestDetectProjectRootFallback(t *testing.T) {
	dir := t.TempDir()
	got := DetectProjectRoot(dir)
	if got != dir {
		t.Errorf("DetectProjectRoot fallback = %q, want %q", got, dir)
	}
}

// TestLoadInstructions verifies cascade loading of instruction files.
func TestLoadInstructions(t *testing.T) {
	root := t.TempDir()

	writeFile := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(filepath.Join(root, "CLAUDE.md"), "# Claude instructions")
	writeFile(filepath.Join(root, "AGENTS.md"), "# Agents instructions")
	// ALTCODE.md intentionally omitted to test skip behaviour.
	writeFile(filepath.Join(root, ".altcode", "rules", "01-style.md"), "# Style rules")
	writeFile(filepath.Join(root, ".altcode", "rules", "02-security.md"), "# Security rules")

	instructions, err := LoadInstructions(root)
	if err != nil {
		t.Fatalf("LoadInstructions returned error: %v", err)
	}

	// We expect: CLAUDE.md, AGENTS.md, 01-style.md, 02-security.md
	// (no global instructions file in temp env, no ALTCODE.md)
	wantPaths := []string{
		filepath.Join(root, "CLAUDE.md"),
		filepath.Join(root, "AGENTS.md"),
		filepath.Join(root, ".altcode", "rules", "01-style.md"),
		filepath.Join(root, ".altcode", "rules", "02-security.md"),
	}

	// Filter out any global file that may or may not exist on this machine.
	var gotPaths []string
	for _, inst := range instructions {
		if inst.Path == globalInstructionsPath() {
			continue
		}
		gotPaths = append(gotPaths, inst.Path)
	}

	if len(gotPaths) != len(wantPaths) {
		t.Fatalf("got %d instructions, want %d:\n%v", len(gotPaths), len(wantPaths), gotPaths)
	}
	for i, p := range wantPaths {
		if gotPaths[i] != p {
			t.Errorf("instructions[%d].Path = %q, want %q", i, gotPaths[i], p)
		}
	}

	// Verify contents are loaded.
	if instructions[0].Content == "" {
		t.Error("first instruction content should not be empty")
	}
}
