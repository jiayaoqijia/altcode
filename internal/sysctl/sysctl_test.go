package sysctl_test

import (
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/config"
	"github.com/jiayaoqijia/altcode/internal/sysctl"
	"github.com/jiayaoqijia/altcode/internal/tool"
)

func TestDetectEnv(t *testing.T) {
	env := sysctl.DetectEnv()
	if env.WorkDir == "" {
		t.Error("WorkDir should not be empty")
	}
	if env.Date == "" {
		t.Error("Date should not be empty")
	}
	if env.Platform == "" {
		t.Error("Platform should not be empty")
	}
	// Platform should be os/arch format
	if !strings.Contains(env.Platform, "/") {
		t.Errorf("Platform should be os/arch, got %q", env.Platform)
	}
}

func TestBuildSystemPrompt_Basic(t *testing.T) {
	cfg := config.Default()
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool())
	registry.Register(tool.NewGrepTool())

	env := sysctl.EnvContext{
		WorkDir:  "/tmp/test",
		Date:     "2025-01-01",
		Platform: "linux/amd64",
	}

	sections := sysctl.BuildSystemPrompt(cfg, registry, nil, env)
	if len(sections) < 3 {
		t.Fatalf("Expected at least 3 sections (persona, tools, env), got %d", len(sections))
	}

	// First section should be the core persona
	if !strings.Contains(sections[0].Content, "altcode") {
		t.Error("First section should contain core persona")
	}

	// Second section should list tools
	if !strings.Contains(sections[1].Content, "Tools") {
		t.Error("Second section should list tools")
	}

	// Last section should have environment info
	lastSection := sections[len(sections)-1]
	if !strings.Contains(lastSection.Content, "/tmp/test") {
		t.Error("Last section should contain working directory")
	}
	if !strings.Contains(lastSection.Content, "2025-01-01") {
		t.Error("Last section should contain date")
	}
}

func TestBuildSystemPrompt_WithInstructions(t *testing.T) {
	cfg := config.Default()
	registry := tool.NewRegistry()
	env := sysctl.EnvContext{WorkDir: "/tmp", Date: "2025-01-01", Platform: "linux/amd64"}

	instructions := []config.Instruction{
		{Path: "CLAUDE.md", Content: "Always use Go."},
		{Path: ".claude/settings.md", Content: "Be concise."},
	}

	sections := sysctl.BuildSystemPrompt(cfg, registry, instructions, env)

	// Should have persona + tools + 2 instructions + env = 5 sections
	if len(sections) < 5 {
		t.Fatalf("Expected at least 5 sections, got %d", len(sections))
	}

	// Find instruction sections
	foundClaude := false
	foundSettings := false
	for _, s := range sections {
		if strings.Contains(s.Content, "CLAUDE.md") && strings.Contains(s.Content, "Always use Go") {
			foundClaude = true
		}
		if strings.Contains(s.Content, "settings.md") && strings.Contains(s.Content, "Be concise") {
			foundSettings = true
		}
	}
	if !foundClaude {
		t.Error("Should include CLAUDE.md instruction")
	}
	if !foundSettings {
		t.Error("Should include settings.md instruction")
	}
}

func TestBuildSystemPrompt_EmptyRegistry(t *testing.T) {
	cfg := config.Default()
	registry := tool.NewRegistry()
	env := sysctl.EnvContext{WorkDir: "/", Date: "2025-01-01", Platform: "linux/amd64"}

	sections := sysctl.BuildSystemPrompt(cfg, registry, nil, env)
	if len(sections) < 3 {
		t.Fatalf("Expected at least 3 sections even with no tools, got %d", len(sections))
	}
}

func TestBuildSystemPrompt_CacheControlOnStaticSections(t *testing.T) {
	cfg := config.Default()
	registry := tool.NewRegistry()
	env := sysctl.EnvContext{WorkDir: "/", Date: "2025-01-01", Platform: "linux/amd64"}

	sections := sysctl.BuildSystemPrompt(cfg, registry, nil, env)

	// Persona and tools should have cache control
	if sections[0].CacheControl == nil {
		t.Error("Persona section should have cache control")
	}
	if sections[1].CacheControl == nil {
		t.Error("Tools section should have cache control")
	}

	// Environment (last section) should NOT have cache control
	lastSection := sections[len(sections)-1]
	if lastSection.CacheControl != nil {
		t.Error("Environment section should not have cache control (it's dynamic)")
	}
}
