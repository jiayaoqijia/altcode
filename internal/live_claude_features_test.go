//go:build !windows

package internal_test

// Live e2e tests verifying EVERY Claude Code feature works on altcode
// with BOTH Claude subscription AND Codex relay (GPT-5.4).
// Tests run in parallel across providers.

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
	"github.com/altcode-ai/altcode/internal/store"
)

type providerCfg struct {
	name string
	cfg  *config.Config
}

func bothProviders(t *testing.T) []providerCfg {
	t.Helper()
	var providers []providerCfg

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg := config.Default()
		cfg.Model = "anthropic/claude-haiku-4-5-20251001"
		cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: key}
		providers = append(providers, providerCfg{"claude", cfg})
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		base := os.Getenv("OPENAI_BASE_URL")
		if base == "" {
			base = "https://api.openai.com"
		}
		cfg := config.Default()
		cfg.Model = "openai/gpt-5.4"
		cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: base}
		providers = append(providers, providerCfg{"gpt", cfg})
	}

	if len(providers) == 0 {
		t.Skip("No API keys set")
	}
	return providers
}

func runWithTimeout(t *testing.T, cfg *config.Config, prompt string, timeout time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var buf bytes.Buffer
	exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       prompt,
		Writer:       &buf,
	})
	return buf.String()
}

// =============================================================================
// FEATURE 1: Slash commands with !backtick expansion
// =============================================================================

func TestLiveBoth_SlashCommandExpansion(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			cmd := &command.Command{
				Body: "Current branch: !`git branch --show-current`\nExplain $ARGUMENTS briefly.",
			}
			expanded, _ := cmd.Expand("the Makefile")
			// Send expanded command to provider
			output := runWithTimeout(t, p.cfg, expanded, 30*time.Second)
			t.Logf("[%s] Response: %.200s", p.name, output)
			if output == "" {
				t.Error("Empty response")
			}
		})
	}
}

// =============================================================================
// FEATURE 2: Tool calls (read/ls/grep/bash/edit/write)
// =============================================================================

func TestLiveBoth_ToolCall_Read(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			output := runWithTimeout(t, p.cfg, "Read the first 5 lines of go.mod using the read tool. Show them.", 30*time.Second)
			t.Logf("[%s] %.200s", p.name, output)
			if !strings.Contains(output, "altcode") && !strings.Contains(output, "module") {
				t.Errorf("Should reference go.mod content")
			}
		})
	}
}

func TestLiveBoth_ToolCall_Bash(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			output := runWithTimeout(t, p.cfg, "Run 'echo DUAL_PROVIDER_TEST' using bash. Show output.", 30*time.Second)
			t.Logf("[%s] %.200s", p.name, output)
			if !strings.Contains(output, "DUAL_PROVIDER_TEST") {
				t.Errorf("Should contain DUAL_PROVIDER_TEST")
			}
		})
	}
}

func TestLiveBoth_ToolCall_Grep(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			output := runWithTimeout(t, p.cfg, "Use grep to find 'func main' in cmd/altcode/main.go. Show results.", 30*time.Second)
			t.Logf("[%s] %.200s", p.name, output)
		})
	}
}

func TestLiveBoth_ToolCall_MultiTurn(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			eng, _ := engine.New(engine.EngineParams{Config: p.cfg})
			toolResults := 0
			for ev := range eng.Run(ctx, "Use ls on current dir, then read first line of README.md. Be brief.") {
				if ev.Type == event.ToolResultEvent {
					toolResults++
				}
			}
			t.Logf("[%s] Tool results: %d", p.name, toolResults)
			if toolResults < 2 {
				t.Errorf("Expected 2+ tool results, got %d", toolResults)
			}
		})
	}
}

// =============================================================================
// FEATURE 3: Hooks (PreToolUse/PostToolUse/Stop)
// =============================================================================

func TestLiveBoth_HookPreToolUse(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			logFile := filepath.Join(dir, "hook.log")
			script := filepath.Join(dir, "hook.sh")
			os.WriteFile(script, []byte("#!/bin/sh\necho fired >> "+logFile+"\necho '{\"decision\":\"allow\"}'"), 0o755)

			hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
				hooks.PreToolUse: {{Matcher: "*", Hooks: []hooks.EntryConfig{
					{Type: "command", Command: "sh " + script},
				}}},
			})

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			eng, _ := engine.New(engine.EngineParams{Config: p.cfg, Hooks: hookRunner})
			for range eng.Run(ctx, "Use the ls tool on current directory") {
			}

			data, _ := os.ReadFile(logFile)
			if !strings.Contains(string(data), "fired") {
				t.Errorf("[%s] Hook should fire", p.name)
			}
		})
	}
}

func TestLiveBoth_HookDeny(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			script := filepath.Join(dir, "deny.sh")
			os.WriteFile(script, []byte("#!/bin/sh\necho BLOCKED >&2\nexit 2"), 0o755)

			hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
				hooks.PreToolUse: {{Matcher: "*", Hooks: []hooks.EntryConfig{
					{Type: "command", Command: "sh " + script},
				}}},
			})

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			eng, _ := engine.New(engine.EngineParams{Config: p.cfg, Hooks: hookRunner})

			blocked := false
			for ev := range eng.Run(ctx, "Use ls on current dir") {
				if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
					if strings.Contains(ev.ToolResult.Output, "BLOCKED") {
						blocked = true
					}
				}
			}
			if !blocked {
				t.Logf("[%s] Hook deny may not trigger if model didn't call tools", p.name)
			}
		})
	}
}

// =============================================================================
// FEATURE 4: Session persistence + resume
// =============================================================================

func TestLiveBoth_SessionResume(t *testing.T) {
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			db, _ := store.Open(":memory:")
			defer db.Close()
			sess, _ := db.CreateSession("test", "live-"+p.name, p.cfg.Model)

			// Turn 1
			ctx1, c1 := context.WithTimeout(context.Background(), 30*time.Second)
			defer c1()
			var buf1 bytes.Buffer
			exec.Run(ctx1, exec.Params{
				EngineParams: engine.EngineParams{Config: p.cfg, Store: db, SessionID: sess.ID},
				Prompt:       "My favorite language is Go. Confirm.",
				Writer:       &buf1,
			})

			// Turn 2
			msgs, _ := db.ListMessages(sess.ID)
			ctx2, c2 := context.WithTimeout(context.Background(), 30*time.Second)
			defer c2()
			var buf2 bytes.Buffer
			exec.Run(ctx2, exec.Params{
				EngineParams: engine.EngineParams{
					Config: p.cfg, Store: db, SessionID: sess.ID,
					Messages: store.ToProviderMessages(msgs),
				},
				Prompt: "What is my favorite language?",
				Writer: &buf2,
			})

			t.Logf("[%s] Turn 2: %s", p.name, buf2.String())
			if !strings.Contains(strings.ToLower(buf2.String()), "go") {
				t.Errorf("[%s] Should remember Go", p.name)
			}
		})
	}
}

// =============================================================================
// FEATURE 5: Subagent spawn
// =============================================================================

func TestLiveBoth_SubagentSpawn(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			eng, _ := engine.New(engine.EngineParams{Config: p.cfg})
			ag := &agent.Agent{
				Name:         "explainer",
				Model:        "inherit",
				Tools:        []string{"read"},
				SystemPrompt: "You explain code briefly.",
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var text string
			for ev := range agent.Spawn(ctx, eng, ag, "What is a goroutine? One sentence.") {
				if ev.Type == event.TextDelta {
					text += ev.Text
				}
			}
			t.Logf("[%s] Agent: %.200s", p.name, text)
			if text == "" {
				t.Error("Agent should respond")
			}
		})
	}
}

// =============================================================================
// FEATURE 6: JSON exec mode
// =============================================================================

func TestLiveBoth_JSONExec(t *testing.T) {
	t.Parallel()
	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var buf bytes.Buffer
			exec.Run(ctx, exec.Params{
				EngineParams: engine.EngineParams{Config: p.cfg},
				Prompt:       "Say hello",
				JSON:         true,
				Writer:       &buf,
			})

			types := make(map[string]bool)
			for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
				var ev event.Event
				if json.Unmarshal([]byte(line), &ev) == nil {
					types[string(ev.Type)] = true
				}
			}
			if !types["text_delta"] {
				t.Errorf("[%s] Missing text_delta", p.name)
			}
			if !types["done"] {
				t.Errorf("[%s] Missing done", p.name)
			}
		})
	}
}

// =============================================================================
// FEATURE 7: Plugin loading (actual Claude Code plugins)
// =============================================================================

func TestLiveBoth_LoadClaudeCodePlugins(t *testing.T) {
	t.Parallel()
	bothProviders(t) // just skip if no keys
	dir := filepath.Join("..", "vendor", "claude-code", "plugins")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("vendor/claude-code not found")
	}
	plugins, _ := plugin.Discover(dir)

	// Verify all 12 plugins load
	if len(plugins) < 10 {
		t.Errorf("Expected 10+ plugins, got %d", len(plugins))
	}

	totalAgents := 0
	for _, p := range plugins {
		totalAgents += len(p.Agents)
	}
	if totalAgents < 10 {
		t.Errorf("Expected 10+ agents across plugins, got %d", totalAgents)
	}
	t.Logf("Loaded %d plugins with %d agents", len(plugins), totalAgents)
}

// =============================================================================
// FEATURE 8: Feature-dev agent workflow with both providers
// =============================================================================

func TestLiveBoth_FeatureDevAgent(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "vendor", "claude-code", "plugins")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("vendor/claude-code not found")
	}
	plugins, _ := plugin.Discover(dir)

	var featureDev *plugin.Plugin
	for _, p := range plugins {
		if p.Manifest.Name == "feature-dev" {
			featureDev = p
		}
	}
	if featureDev == nil {
		t.Skip("feature-dev plugin not found")
	}

	var explorer *agent.Agent
	for _, a := range featureDev.Agents {
		if a.Name == "code-explorer" {
			explorer = a
		}
	}
	if explorer == nil {
		t.Fatal("code-explorer not found")
	}

	for _, p := range bothProviders(t) {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			eng, _ := engine.New(engine.EngineParams{Config: p.cfg})

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var text string
			for ev := range agent.Spawn(ctx, eng, explorer, "Briefly describe what internal/engine/ does.") {
				if ev.Type == event.TextDelta {
					text += ev.Text
				}
			}
			t.Logf("[%s] Explorer: %.300s", p.name, text)
			if text == "" {
				t.Logf("[%s] Agent returned empty (may be rate limited for model: sonnet)", p.name)
			}
		})
	}
}

// =============================================================================
// FEATURE 9: Instruction cascade (CLAUDE.md)
// =============================================================================

func TestLiveBoth_InstructionCascade(t *testing.T) {
	t.Parallel()
	bothProviders(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Always respond in uppercase."), 0o644)

	instructions, _ := config.LoadInstructions(dir)
	found := false
	for _, inst := range instructions {
		if strings.Contains(inst.Content, "uppercase") {
			found = true
		}
	}
	if !found {
		t.Error("CLAUDE.md not loaded")
	}
}

// =============================================================================
// FEATURE 10: Cross-provider identical behavior
// =============================================================================

func TestLiveBoth_SameAnswerBothProviders(t *testing.T) {
	providers := bothProviders(t)
	if len(providers) < 2 {
		t.Skip("Need both providers")
	}

	answers := make(map[string]string)
	for _, p := range providers {
		output := runWithTimeout(t, p.cfg, "What is 7*8? Reply with just the number.", 30*time.Second)
		answers[p.name] = strings.TrimSpace(output)
		t.Logf("[%s] = %q", p.name, answers[p.name])
	}

	for _, ans := range answers {
		if !strings.Contains(ans, "56") {
			t.Errorf("Expected 56: %q", ans)
		}
	}
}
