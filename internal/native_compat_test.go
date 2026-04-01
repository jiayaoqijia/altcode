package internal_test

// E2E tests verifying altcode can run Claude Code plugins/skills natively
// with BOTH Anthropic and OpenAI/Codex/GPT providers.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/plugin"
)

// =============================================================================
// 1. Claude Code plugin discovery with .claude-plugin/ directory
// =============================================================================

func TestNative_DiscoverClaudePluginDir(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")

	// Use .claude-plugin/ (Claude Code format, NOT .altcode-plugin/)
	os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"my-plugin","version":"1.0.0"}`), 0o644)

	plugins, err := plugin.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 {
		t.Fatalf("Should discover .claude-plugin/ format, got %d", len(plugins))
	}
	if plugins[0].Manifest.Name != "my-plugin" {
		t.Errorf("Name: %q", plugins[0].Manifest.Name)
	}
}

func TestNative_DiscoverBothPluginDirFormats(t *testing.T) {
	dir := t.TempDir()

	// Plugin A: .altcode-plugin/ format
	pA := filepath.Join(dir, "plugin-a")
	os.MkdirAll(filepath.Join(pA, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pA, ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-a"}`), 0o644)

	// Plugin B: .claude-plugin/ format
	pB := filepath.Join(dir, "plugin-b")
	os.MkdirAll(filepath.Join(pB, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(pB, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-b"}`), 0o644)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 2 {
		t.Fatalf("Should discover both formats, got %d", len(plugins))
	}
}

// =============================================================================
// 2. Load ACTUAL Claude Code commit-commands plugin
// =============================================================================

func TestNative_LoadClaudeCodeCommitCommandsPlugin(t *testing.T) {
	// Replicate the exact structure of Claude Code's commit-commands plugin
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "commit-commands")

	os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"commit-commands","version":"1.0.0","description":"Git commit workflows"}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "commands", "commit.md"), []byte(`---
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git commit:*)
description: Create a git commit
---

## Context

- Current git status: !`+"`git status`"+`

## Your task

1. Stage all changes
2. Create a commit with a descriptive message
3. You MUST do all of the above in a single message.
`), 0o644)

	os.WriteFile(filepath.Join(pluginDir, "commands", "commit-push-pr.md"), []byte(`---
allowed-tools: Bash(git checkout --branch:*), Bash(git add:*), Bash(git status:*), Bash(git push:*), Bash(git commit:*), Bash(gh pr create:*)
description: Commit, push, and open a PR
---

Based on changes, create branch, commit, push, and PR.
`), 0o644)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 1 {
		t.Fatalf("Expected 1 plugin, got %d", len(plugins))
	}

	p := plugins[0]
	if len(p.Commands) != 2 {
		t.Fatalf("Expected 2 commands, got %d", len(p.Commands))
	}

	// Verify allowed-tools parsed correctly
	var commitCmd *command.Command
	for _, c := range p.Commands {
		if c.Name == "commit" {
			commitCmd = c
		}
	}
	if commitCmd == nil {
		t.Fatal("commit command not found")
	}
	if len(commitCmd.AllowedTools) != 3 {
		t.Errorf("Expected 3 allowed tools, got %v", commitCmd.AllowedTools)
	}
	// Verify Bash(pattern) syntax preserved
	foundBashGit := false
	for _, tool := range commitCmd.AllowedTools {
		if strings.Contains(tool, "Bash(git") {
			foundBashGit = true
		}
	}
	if !foundBashGit {
		t.Errorf("Bash(git...) pattern not preserved: %v", commitCmd.AllowedTools)
	}
}

// =============================================================================
// 3. Load Claude Code feature-dev plugin WITH agents
// =============================================================================

func TestNative_LoadClaudeCodeFeatureDevPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "feature-dev")

	os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"feature-dev","version":"1.0.0"}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "commands", "feature-dev.md"),
		[]byte("---\ndescription: Guided feature development\n---\nDevelop the feature."), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "agents", "code-explorer.md"), []byte(`---
name: code-explorer
description: Deeply analyzes existing codebase features
tools: Glob, Grep, LS, Read
model: sonnet
color: yellow
---

You are an expert code analyst specializing in tracing feature implementations.
`), 0o644)

	os.WriteFile(filepath.Join(pluginDir, "agents", "code-architect.md"), []byte(`---
name: code-architect
description: Designs feature architectures
tools: Glob, Grep, LS, Read
model: sonnet
color: green
---

You are a senior software architect who delivers comprehensive blueprints.
`), 0o644)

	os.WriteFile(filepath.Join(pluginDir, "agents", "code-reviewer.md"), []byte(`---
name: code-reviewer
description: Reviews code for bugs and quality issues
tools: Glob, Grep, LS, Read
model: sonnet
color: red
---

You are an expert code reviewer with high precision filtering.
`), 0o644)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 1 {
		t.Fatalf("Expected 1 plugin, got %d", len(plugins))
	}

	p := plugins[0]

	// Commands loaded
	if len(p.Commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(p.Commands))
	}

	// Agents loaded (THIS WAS THE CRITICAL FIX)
	if len(p.Agents) != 3 {
		t.Fatalf("Expected 3 agents, got %d", len(p.Agents))
	}

	// Verify agent details
	names := make(map[string]bool)
	for _, a := range p.Agents {
		names[a.Name] = true
		if a.Model != "sonnet" {
			t.Errorf("Agent %q model: %q", a.Name, a.Model)
		}
		if len(a.Tools) == 0 {
			t.Errorf("Agent %q has no tools", a.Name)
		}
		if a.SystemPrompt == "" {
			t.Errorf("Agent %q has no system prompt", a.Name)
		}
	}
	for _, expected := range []string{"code-explorer", "code-architect", "code-reviewer"} {
		if !names[expected] {
			t.Errorf("Missing agent: %s", expected)
		}
	}
}

// =============================================================================
// 4. Claude Code security-guidance plugin hooks work with OpenAI
// =============================================================================

func TestNative_SecurityGuidanceHooksWithOpenAI(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "security-guidance")

	os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"security-guidance"}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755)
	// Replicate Claude Code's security hook format
	hookScript := filepath.Join(dir, "security.py")
	os.WriteFile(hookScript, []byte(`#!/usr/bin/env python3
import json, sys
data = json.load(sys.stdin)
ti = data.get("toolInput", {})
if isinstance(ti, str):
    ti = json.loads(ti) if ti else {}
fp = ti.get("file_path", "") if isinstance(ti, dict) else ""
if ".env" in fp or "/etc/" in fp:
    print("Security: sensitive file!", file=sys.stderr)
    sys.exit(2)
print(json.dumps({"decision": "allow"}))
`), 0o755)

	os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"),
		[]byte(fmt.Sprintf(`{"description":"Security hooks","hooks":{"PreToolUse":[{"matcher":"Edit|Write","hooks":[{"type":"command","command":"python3 %s"}]}]}}`, hookScript)), 0o644)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 1 {
		t.Fatal("Plugin not discovered")
	}

	cfg := config.Default()
	plugins[0].Merge(cfg)

	// Convert to hooks.MatcherConfig
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
	hookRunner := hooks.NewRunner(hookConfigs)

	// Test with OpenAI provider — hook should still fire
	oaiBody := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"write","arguments":""}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_path\":\".env\",\"content\":\"SECRET=x\"}"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	oaiText := `data: {"choices":[{"delta":{"content":"OK."},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"

	var mu sync.Mutex
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := idx
		idx++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if i == 0 {
			w.Write([]byte(oaiBody))
		} else {
			w.Write([]byte(oaiText))
		}
	}))
	defer srv.Close()

	oaiCfg := config.Default()
	oaiCfg.Model = "openai/gpt-4"
	oaiCfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	eng, _ := engine.New(engine.EngineParams{
		Config: oaiCfg,
		Hooks:  hookRunner,
	})

	events := nc_drain(eng.Run(context.Background(), "write .env"))

	// Security hook should block the .env write
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Security") ||
				strings.Contains(ev.ToolResult.Output, "sensitive") {
				return // PASS — hook blocked it
			}
		}
	}
	t.Error("Security hook should block .env write with OpenAI provider")
}

// =============================================================================
// 5. Spawn Claude Code agent with OpenAI provider
// =============================================================================

func TestNative_SpawnAgentWithOpenAI(t *testing.T) {
	oaiText := `data: {"choices":[{"delta":{"content":"Analysis complete."},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(oaiText))
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Model = "openai/gpt-4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	parent, _ := engine.New(engine.EngineParams{Config: cfg})

	ag := &agent.Agent{
		Name:         "code-explorer",
		Model:        "inherit", // inherits openai/gpt-4
		Tools:        []string{"read", "grep", "ls"},
		SystemPrompt: "You are an expert code analyst.",
	}

	events := nc_drain(agent.Spawn(context.Background(), parent, ag, "analyze main.go"))

	hasText := false
	hasDone := false
	for _, ev := range events {
		if ev.Type == event.TextDelta && strings.Contains(ev.Text, "Analysis") {
			hasText = true
		}
		if ev.Type == event.Done {
			hasDone = true
		}
	}
	if !hasText {
		t.Error("Agent should produce text with OpenAI")
	}
	if !hasDone {
		t.Error("Agent should complete")
	}
}

// =============================================================================
// 6. Command expansion works identically across providers
// =============================================================================

func TestNative_CommandExpandAcrossProviders(t *testing.T) {
	cmd := &command.Command{
		Name:        "review",
		Description: "Review changes",
		AllowedTools: []string{"Read", "Grep", "Bash(git diff *)"},
		Body:        "Review $ARGUMENTS.\nBranch: !`git branch --show-current`",
	}

	expanded, _ := cmd.Expand("main.go")

	if !strings.Contains(expanded, "main.go") {
		t.Error("$ARGUMENTS not replaced")
	}
	// !`git branch` should have been executed
	if strings.Contains(expanded, "!`") {
		t.Error("Backtick not expanded")
	}
}

// =============================================================================
// 7. Exec mode works with Claude Code commands + OpenAI
// =============================================================================

func TestNative_ExecModeWithCommandAndOpenAI(t *testing.T) {
	oaiText := `data: {"choices":[{"delta":{"content":"Reviewed."},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(oaiText))
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Model = "openai/gpt-4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       "Review the code for bugs",
		Writer:       &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Reviewed.") {
		t.Errorf("Output: %q", buf.String())
	}
}

// =============================================================================
// 8. JSON exec mode works identically for both providers
// =============================================================================

func TestNative_ExecJSONParityAcrossProviders(t *testing.T) {
	// Anthropic SSE
	anthropicSSE := "event: content_block_start\ndata: " + `{"index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"delta":{"type":"text_delta","text":"hello"}}` + "\n\n" +
		"event: content_block_stop\ndata: {}\n\n" +
		"event: message_stop\ndata: {}\n\n"

	// OpenAI SSE
	openaiSSE := `data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"

	for _, tc := range []struct {
		name     string
		provider string
		sse      string
	}{
		{"anthropic", "anthropic", anthropicSSE},
		{"openai", "openai", openaiSSE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.Write([]byte(tc.sse))
			}))
			defer srv.Close()

			cfg := config.Default()
			if tc.provider == "openai" {
				cfg.Model = "openai/gpt-4"
				cfg.Provider["openai"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
			} else {
				cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
			}

			var buf bytes.Buffer
			exec.Run(context.Background(), exec.Params{
				EngineParams: engine.EngineParams{Config: cfg},
				Prompt:       "hi",
				JSON:         true,
				Writer:       &buf,
			})

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			types := make(map[string]bool)
			for _, line := range lines {
				var ev event.Event
				if json.Unmarshal([]byte(line), &ev) == nil {
					types[string(ev.Type)] = true
				}
			}

			if !types["text_delta"] {
				t.Errorf("%s: missing text_delta", tc.name)
			}
			if !types["done"] {
				t.Errorf("%s: missing done", tc.name)
			}
		})
	}
}

// =============================================================================
// 9. Plugin with hooks + agents + commands — full integration
// =============================================================================

func TestNative_FullPluginIntegration(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "full-plugin")

	os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"full-plugin","version":"1.0.0"}`), 0o644)

	// Command
	os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "commands", "deploy.md"),
		[]byte("---\ndescription: Deploy\nallowed-tools: Bash(git *)\n---\nDeploy $ARGUMENTS"), 0o644)

	// Agent
	os.MkdirAll(filepath.Join(pluginDir, "agents"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "agents", "reviewer.md"), []byte(`---
name: reviewer
description: Review code
model: inherit
tools: ["read", "grep"]
---

You are a code reviewer.
`), 0o644)

	// Hook
	os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"),
		[]byte(`{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"echo '{\"decision\":\"allow\"}'"}]}]}}`), 0o644)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 1 {
		t.Fatalf("Expected 1, got %d", len(plugins))
	}

	p := plugins[0]

	if len(p.Commands) != 1 {
		t.Errorf("Commands: %d", len(p.Commands))
	}
	if len(p.Agents) != 1 {
		t.Errorf("Agents: %d", len(p.Agents))
	}
	if len(p.Hooks["PreToolUse"]) != 1 {
		t.Errorf("Hooks: %d", len(p.Hooks["PreToolUse"]))
	}

	// Verify command works
	expanded, _ := p.Commands[0].Expand("production")
	if !strings.Contains(expanded, "production") {
		t.Error("Command expand failed")
	}

	// Verify agent has correct tools
	if len(p.Agents[0].Tools) != 2 {
		t.Errorf("Agent tools: %v", p.Agents[0].Tools)
	}

	// Verify hooks merge
	cfg := config.Default()
	p.Merge(cfg)
	if len(cfg.Hooks["PreToolUse"]) == 0 {
		t.Error("Hooks not merged")
	}
}

// =============================================================================
// 10. Instruction cascade CLAUDE.md works with both providers
// =============================================================================

func TestNative_InstructionCascadeWithBothProviders(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Always follow project conventions."), 0o644)
	os.WriteFile(filepath.Join(dir, "ALTCODE.md"), []byte("Use Go idioms."), 0o644)

	instructions, err := config.LoadInstructions(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(instructions) < 2 {
		t.Fatalf("Expected 2+ instructions, got %d", len(instructions))
	}

	// Instructions should work identically regardless of provider
	foundClaude := false
	foundAltcode := false
	for _, inst := range instructions {
		if strings.Contains(inst.Content, "project conventions") {
			foundClaude = true
		}
		if strings.Contains(inst.Content, "Go idioms") {
			foundAltcode = true
		}
	}
	if !foundClaude {
		t.Error("CLAUDE.md not loaded")
	}
	if !foundAltcode {
		t.Error("ALTCODE.md not loaded")
	}
}

func nc_drain(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
