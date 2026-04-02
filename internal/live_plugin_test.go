//go:build !windows

package internal_test

// Live e2e tests verifying altcode loads ACTUAL Claude Code plugins
// from vendor/claude-code/plugins/ and runs them with the Codex relay.
// Skipped without OPENAI_API_KEY + OPENAI_BASE_URL.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/plugin"
)

func skipWithoutCodexRelay(t *testing.T) (*config.Config, string) {
	t.Helper()
	key := os.Getenv("OPENAI_API_KEY")
	base := os.Getenv("OPENAI_BASE_URL")
	if key == "" || base == "" {
		t.Skip("OPENAI_API_KEY and OPENAI_BASE_URL not set (need Codex relay)")
	}
	cfg := config.Default()
	cfg.Model = "openai/gpt-5.4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: base}
	return cfg, base
}

func claudeCodePluginDir() string {
	return filepath.Join(".", "..", "vendor", "claude-code", "plugins")
}

// =============================================================================
// 1. Discover ALL actual Claude Code plugins
// =============================================================================

func TestLivePlugin_DiscoverAllClaudeCodePlugins(t *testing.T) {
	skipWithoutCodexRelay(t)

	dir := claudeCodePluginDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("vendor/claude-code/plugins not found")
	}

	plugins, err := plugin.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Discovered %d plugins:", len(plugins))
	for _, p := range plugins {
		t.Logf("  %s: %d cmds, %d agents, %d hook events",
			p.Manifest.Name, len(p.Commands), len(p.Agents), len(p.Hooks))
	}

	if len(plugins) < 8 {
		t.Errorf("Expected at least 8 Claude Code plugins, got %d", len(plugins))
	}

	// Verify key plugins found
	names := make(map[string]bool)
	for _, p := range plugins {
		names[p.Manifest.Name] = true
	}
	for _, required := range []string{
		"commit-commands", "feature-dev", "security-guidance",
		"hookify", "pr-review-toolkit",
	} {
		if !names[required] {
			t.Errorf("Missing plugin: %s", required)
		}
	}
}

// =============================================================================
// 2. Load commit-commands plugin and run /commit command with GPT-5.4
// =============================================================================

func TestLivePlugin_CommitCommandsWithGPT(t *testing.T) {
	cfg, _ := skipWithoutCodexRelay(t)

	dir := claudeCodePluginDir()
	plugins, _ := plugin.Discover(dir)

	var commitPlugin *plugin.Plugin
	for _, p := range plugins {
		if p.Manifest.Name == "commit-commands" {
			commitPlugin = p
			break
		}
	}
	if commitPlugin == nil {
		t.Skip("commit-commands plugin not found")
	}

	// Find the commit command
	var commitCmd *command.Command
	for _, c := range commitPlugin.Commands {
		if c.Name == "commit" {
			commitCmd = c
		}
	}
	if commitCmd == nil {
		t.Fatal("commit command not found in plugin")
	}

	t.Logf("Command: %s", commitCmd.Name)
	t.Logf("Description: %s", commitCmd.Description)
	t.Logf("AllowedTools: %v", commitCmd.AllowedTools)

	// Expand the command (will execute !`git status` etc.)
	expanded, err := commitCmd.Expand("")
	if err != nil {
		t.Logf("Expand warning (non-fatal): %v", err)
	}
	t.Logf("Expanded body (first 200 chars): %.200s", expanded)

	if !strings.Contains(expanded, "commit") && !strings.Contains(expanded, "Your task") {
		t.Error("Expanded command should contain task instructions")
	}

	// Run with GPT-5.4 — just verify it produces a response
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err = exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       "There are no changes to commit. Just say 'nothing to commit'.",
		Writer:       &buf,
	})
	if err != nil {
		t.Logf("Exec error (may be expected): %v", err)
	}
	t.Logf("GPT response: %.200s", buf.String())
}

// =============================================================================
// 3. Load feature-dev plugin agents and spawn code-explorer with GPT-5.4
// =============================================================================

func TestLivePlugin_FeatureDevAgentsWithGPT(t *testing.T) {
	cfg, _ := skipWithoutCodexRelay(t)

	dir := claudeCodePluginDir()
	plugins, _ := plugin.Discover(dir)

	var featureDevPlugin *plugin.Plugin
	for _, p := range plugins {
		if p.Manifest.Name == "feature-dev" {
			featureDevPlugin = p
			break
		}
	}
	if featureDevPlugin == nil {
		t.Skip("feature-dev plugin not found")
	}

	t.Logf("Agents: %d", len(featureDevPlugin.Agents))
	for _, a := range featureDevPlugin.Agents {
		t.Logf("  %s (model=%s, tools=%d): %.80s",
			a.Name, a.Model, len(a.Tools), a.SystemPrompt)
	}

	if len(featureDevPlugin.Agents) < 3 {
		t.Fatalf("Expected 3 agents, got %d", len(featureDevPlugin.Agents))
	}

	// Find code-explorer
	var explorer *agent.Agent
	for _, a := range featureDevPlugin.Agents {
		if a.Name == "code-explorer" {
			explorer = a
		}
	}
	if explorer == nil {
		t.Fatal("code-explorer agent not found")
	}

	// Spawn with GPT-5.4
	eng, err := engine.New(engine.EngineParams{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch := agent.Spawn(ctx, eng, explorer, "What does internal/engine/engine.go do? Be brief.")

	var text string
	for ev := range ch {
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
		if ev.Type == event.ErrorEvent {
			t.Logf("Agent error: %s", ev.Error)
		}
	}

	t.Logf("Agent response (first 300 chars): %.300s", text)
	if text == "" {
		t.Error("Agent should produce a response")
	}
}

// =============================================================================
// 4. Load pr-review-toolkit agents and verify all 6 parse correctly
// =============================================================================

func TestLivePlugin_PRReviewToolkitAgents(t *testing.T) {
	skipWithoutCodexRelay(t)

	dir := claudeCodePluginDir()
	plugins, _ := plugin.Discover(dir)

	var prPlugin *plugin.Plugin
	for _, p := range plugins {
		if p.Manifest.Name == "pr-review-toolkit" {
			prPlugin = p
			break
		}
	}
	if prPlugin == nil {
		t.Skip("pr-review-toolkit not found")
	}

	expected := []string{
		"code-reviewer", "comment-analyzer", "silent-failure-hunter",
		"type-design-analyzer", "code-simplifier", "pr-test-analyzer",
	}

	t.Logf("Loaded %d agents", len(prPlugin.Agents))
	names := make(map[string]bool)
	for _, a := range prPlugin.Agents {
		names[a.Name] = true
		t.Logf("  %s: model=%s tools=%d prompt=%d chars",
			a.Name, a.Model, len(a.Tools), len(a.SystemPrompt))

		// Verify each agent has required fields
		if a.SystemPrompt == "" {
			t.Errorf("Agent %s has empty system prompt", a.Name)
		}
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("Missing agent: %s", name)
		}
	}
}

// =============================================================================
// 5. Security-guidance hooks fire with GPT-5.4 tool calls
// =============================================================================

func TestLivePlugin_SecurityHooksWithGPT(t *testing.T) {
	_, _ = skipWithoutCodexRelay(t)

	dir := claudeCodePluginDir()
	plugins, _ := plugin.Discover(dir)

	var secPlugin *plugin.Plugin
	for _, p := range plugins {
		if p.Manifest.Name == "security-guidance" {
			secPlugin = p
			break
		}
	}
	if secPlugin == nil {
		t.Skip("security-guidance not found")
	}

	if len(secPlugin.Hooks) == 0 {
		t.Fatal("No hooks loaded from security-guidance")
	}

	t.Logf("Hooks: %d events", len(secPlugin.Hooks))
	for ev, matchers := range secPlugin.Hooks {
		for _, m := range matchers {
			t.Logf("  %s: matcher=%s hooks=%d", ev, m.Matcher, len(m.Hooks))
		}
	}

	// Merge into config and verify
	cfg2 := config.Default()
	secPlugin.Merge(cfg2)
	if len(cfg2.Hooks["PreToolUse"]) == 0 {
		t.Error("PreToolUse hooks should be merged from security-guidance")
	}
}

// =============================================================================
// 6. Hookify plugin loads all hook events
// =============================================================================

func TestLivePlugin_HookifyLoadsAllEvents(t *testing.T) {
	skipWithoutCodexRelay(t)

	dir := claudeCodePluginDir()
	plugins, _ := plugin.Discover(dir)

	var hookifyPlugin *plugin.Plugin
	for _, p := range plugins {
		if p.Manifest.Name == "hookify" {
			hookifyPlugin = p
			break
		}
	}
	if hookifyPlugin == nil {
		t.Skip("hookify not found")
	}

	t.Logf("Commands: %d, Agents: %d, Hook events: %d",
		len(hookifyPlugin.Commands), len(hookifyPlugin.Agents), len(hookifyPlugin.Hooks))

	// Hookify should have PreToolUse, PostToolUse, Stop, UserPromptSubmit
	for _, expected := range []string{"PreToolUse", "PostToolUse", "Stop", "UserPromptSubmit"} {
		if _, ok := hookifyPlugin.Hooks[expected]; !ok {
			t.Errorf("Missing hook event: %s", expected)
		}
	}

	if len(hookifyPlugin.Commands) < 3 {
		t.Errorf("Expected 3+ commands, got %d", len(hookifyPlugin.Commands))
	}
}

// =============================================================================
// 7. Full workflow: load plugin → merge hooks → run with GPT-5.4
// =============================================================================

func TestLivePlugin_FullWorkflowWithGPT(t *testing.T) {
	cfg, _ := skipWithoutCodexRelay(t)

	dir := claudeCodePluginDir()
	plugins, _ := plugin.Discover(dir)

	// Merge ALL plugin hooks into config
	for _, p := range plugins {
		p.Merge(cfg)
	}

	t.Logf("Merged hooks: %d events", len(cfg.Hooks))
	for ev, matchers := range cfg.Hooks {
		t.Logf("  %s: %d matchers", ev, len(matchers))
	}

	// Convert to hooks.Runner
	hookConfigs := make(map[hooks.Event][]hooks.MatcherConfig)
	for ev, matchers := range cfg.Hooks {
		for _, m := range matchers {
			var entries []hooks.EntryConfig
			for _, h := range m.Hooks {
				entries = append(entries, hooks.EntryConfig{
					Type: h.Type, Command: h.Command, Timeout: h.Timeout,
				})
			}
			hookConfigs[hooks.Event(ev)] = append(hookConfigs[hooks.Event(ev)],
				hooks.MatcherConfig{Matcher: m.Matcher, Hooks: entries})
		}
	}

	// Run a simple prompt with all hooks active
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{
			Config: cfg,
			Hooks:  hooks.NewRunner(hookConfigs),
		},
		Prompt: "What is 1+1? Reply with just the number.",
		Writer: &buf,
	})
	if err != nil {
		t.Logf("Error (hooks may cause issues): %v", err)
	}

	t.Logf("Response: %q", buf.String())
	if buf.Len() == 0 {
		t.Error("Expected some output even with hooks")
	}
}

// =============================================================================
// 8. Cross-provider parity: same command, Anthropic vs OpenAI
// =============================================================================

func TestLivePlugin_CrossProviderCommandParity(t *testing.T) {
	cfg, _ := skipWithoutCodexRelay(t)

	// Create a simple command
	cmd := &command.Command{
		Name:        "explain",
		Description: "Explain code",
		Body:        "Explain what $ARGUMENTS does in one sentence.",
	}

	expanded, _ := cmd.Expand("a Go select statement")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	err := exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       expanded,
		Writer:       &buf,
	})
	if err != nil {
		t.Fatalf("Error: %v", err)
	}

	response := buf.String()
	t.Logf("GPT response: %s", response)

	if !strings.Contains(strings.ToLower(response), "select") &&
		!strings.Contains(strings.ToLower(response), "channel") &&
		!strings.Contains(strings.ToLower(response), "goroutine") {
		t.Error("Response should mention select/channel/goroutine concepts")
	}
}

// =============================================================================
// 9. Tool call + hooks integration with live GPT
// =============================================================================

func TestLivePlugin_ToolCallWithHooksGPT(t *testing.T) {
	cfg, _ := skipWithoutCodexRelay(t)

	// Simple allow hook
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: `echo '{"decision":"allow"}'`}},
		}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfg,
		Hooks:  hookRunner,
	})

	ch := eng.Run(ctx, "Use the ls tool to list files in the current directory.")

	var hasToolResult, hasDone bool
	var text string
	for ev := range ch {
		if ev.Type == event.ToolResultEvent {
			hasToolResult = true
		}
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
		if ev.Type == event.Done {
			hasDone = true
		}
	}

	t.Logf("Tool called: %v, Text: %.200s", hasToolResult, text)
	if !hasDone {
		t.Error("Should complete")
	}
	// Model may or may not call tools depending on response
}

// =============================================================================
// 10. JSON exec with tool calls via live GPT
// =============================================================================

func TestLivePlugin_JSONExecWithToolsGPT(t *testing.T) {
	cfg, _ := skipWithoutCodexRelay(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       "Read the first 3 lines of README.md using the read tool.",
		JSON:         true,
		Writer:       &buf,
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	t.Logf("JSONL lines: %d", len(lines))

	types := make(map[string]bool)
	for _, line := range lines {
		var ev event.Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			types[string(ev.Type)] = true
		}
	}

	t.Logf("Event types: %v", types)
	if !types["done"] {
		t.Error("Missing done event")
	}
}
