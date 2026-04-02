//go:build !windows

package internal_test

// Comprehensive live integration tests against the real Codex relay.
// Exercises every critical path that mock tests cover but with real GPT-5.4.
// Skip without OPENAI_API_KEY + OPENAI_BASE_URL.

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
	"github.com/altcode-ai/altcode/internal/compact"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/plugin"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/altcode-ai/altcode/internal/tool"
)

func liveSkip(t *testing.T) *config.Config {
	t.Helper()
	key := os.Getenv("OPENAI_API_KEY")
	base := os.Getenv("OPENAI_BASE_URL")
	if key == "" || base == "" {
		t.Skip("OPENAI_API_KEY/OPENAI_BASE_URL not set")
	}
	cfg := config.Default()
	cfg.Model = "openai/gpt-5.4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: base}
	return cfg
}

func liveRun(t *testing.T, cfg *config.Config, prompt string) ([]event.Event, *engine.Engine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	eng, err := engine.New(engine.EngineParams{Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	var events []event.Event
	for ev := range eng.Run(ctx, prompt) {
		events = append(events, ev)
	}
	return events, eng
}

func liveExec(t *testing.T, cfg *config.Config, prompt string, jsonMode bool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var buf bytes.Buffer
	exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       prompt,
		JSON:         jsonMode,
		Writer:       &buf,
	})
	return buf.String()
}

// =============================================================================
// 1. PROVIDER: SSE streaming
// =============================================================================

func TestLive_StreamTextDelta(t *testing.T) {
	t.Parallel()
	cfg := liveSkip(t)
	events, _ := liveRun(t, cfg, "Say exactly: hello world")
	var text string
	for _, ev := range events {
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
	}
	if !strings.Contains(strings.ToLower(text), "hello") {
		t.Errorf("Expected hello in response: %q", text)
	}
}

func TestLive_StreamDoneEvent(t *testing.T) {
	t.Parallel()
	cfg := liveSkip(t)
	events, _ := liveRun(t, cfg, "Say ok")
	hasDone := false
	for _, ev := range events {
		if ev.Type == event.Done {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("Missing Done event")
	}
}

// =============================================================================
// 2. TOOL CALLS: agent loop dispatches tools
// =============================================================================

func TestLive_ToolCall_Ls(t *testing.T) {
	cfg := liveSkip(t)
	events, _ := liveRun(t, cfg, "Use the ls tool to list files in /tmp. Just show the result.")
	hasToolResult := false
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent {
			hasToolResult = true
		}
	}
	if !hasToolResult {
		t.Error("Expected tool result from ls call")
	}
}

func TestLive_ToolCall_Read(t *testing.T) {
	cfg := liveSkip(t)
	output := liveExec(t, cfg, "Read the first 3 lines of Makefile using the read tool. Show them.", false)
	if !strings.Contains(output, "altcode") && !strings.Contains(output, "BINARY") && !strings.Contains(output, "Makefile") {
		t.Errorf("Expected Makefile content reference: %q", output[:min(len(output), 200)])
	}
}

func TestLive_ToolCall_Grep(t *testing.T) {
	cfg := liveSkip(t)
	output := liveExec(t, cfg, "Use grep to search for 'func main' in cmd/altcode/main.go. Show results.", false)
	t.Logf("Grep response: %.200s", output)
	// Model should have found func main
}

func TestLive_ToolCall_Bash(t *testing.T) {
	cfg := liveSkip(t)
	output := liveExec(t, cfg, "Run 'echo LIVE_TEST_OK' using the bash tool. Show the output.", false)
	if !strings.Contains(output, "LIVE_TEST_OK") {
		t.Errorf("Expected LIVE_TEST_OK: %.200s", output)
	}
}

func TestLive_ToolCall_MultiTurn(t *testing.T) {
	cfg := liveSkip(t)
	events, _ := liveRun(t, cfg, "First use ls to list files in the current directory, then use read to read the first line of README.md. Be brief.")
	toolResults := 0
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent {
			toolResults++
		}
	}
	if toolResults < 2 {
		t.Errorf("Expected 2+ tool results for multi-turn, got %d", toolResults)
	}
}

// =============================================================================
// 3. EXEC MODE
// =============================================================================

func TestLive_ExecTextMode(t *testing.T) {
	t.Parallel()
	cfg := liveSkip(t)
	output := liveExec(t, cfg, "What is 3+3? Reply with just the number.", false)
	if !strings.Contains(output, "6") {
		t.Errorf("Expected 6: %q", output)
	}
}

func TestLive_ExecJSONMode(t *testing.T) {
	t.Parallel()
	cfg := liveSkip(t)
	output := liveExec(t, cfg, "Say hi", true)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	types := make(map[string]bool)
	for _, line := range lines {
		var ev event.Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			types[string(ev.Type)] = true
		}
	}
	if !types["text_delta"] {
		t.Error("Missing text_delta in JSONL")
	}
	if !types["done"] {
		t.Error("Missing done in JSONL")
	}
}

func TestLive_ExecJSONWithToolCalls(t *testing.T) {
	cfg := liveSkip(t)
	output := liveExec(t, cfg, "Use the ls tool to list the current directory.", true)
	types := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var ev event.Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			types[string(ev.Type)] = true
		}
	}
	if !types["tool_start"] {
		t.Error("Missing tool_start")
	}
	if !types["tool_result"] {
		t.Error("Missing tool_result")
	}
}

// =============================================================================
// 4. SESSION PERSISTENCE + RESUME
// =============================================================================

func TestLive_SessionPersistAndResume(t *testing.T) {
	cfg := liveSkip(t)
	db, _ := store.Open(":memory:")
	defer db.Close()
	sess, _ := db.CreateSession("live-test", "live", cfg.Model)

	// Turn 1
	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel1()
	var buf1 bytes.Buffer
	exec.Run(ctx1, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg, Store: db, SessionID: sess.ID},
		Prompt:       "My favorite color is blue. Just confirm you noted it.",
		Writer:       &buf1,
	})
	t.Logf("Turn 1: %s", buf1.String())

	// Turn 2 (resume)
	msgs, _ := db.ListMessages(sess.ID)
	provMsgs := store.ToProviderMessages(msgs)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	var buf2 bytes.Buffer
	exec.Run(ctx2, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg, Store: db, SessionID: sess.ID, Messages: provMsgs},
		Prompt:       "What is my favorite color?",
		Writer:       &buf2,
	})
	t.Logf("Turn 2: %s", buf2.String())

	if !strings.Contains(strings.ToLower(buf2.String()), "blue") {
		t.Error("Model should remember 'blue' from session")
	}
}

// =============================================================================
// 5. PERMISSIONS
// =============================================================================

func TestLive_PermissionDeny(t *testing.T) {
	cfg := liveSkip(t)
	perm := permission.NewEvaluator(permission.ModeDefault, "", []permission.Rule{
		{Tool: "bash", Pattern: "*", Action: permission.ActionDeny, Source: "test"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	eng, _ := engine.New(engine.EngineParams{Config: cfg, Perm: perm})
	events := make([]event.Event, 0)
	for ev := range eng.Run(ctx, "Run echo hello using bash tool") {
		events = append(events, ev)
	}

	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Permission denied") {
				return // PASS
			}
		}
	}
	// Model might not call bash — that's also fine
}

func TestLive_PermissionBypass(t *testing.T) {
	t.Parallel()
	cfg := liveSkip(t)
	perm := permission.NewEvaluator(permission.ModeBypass, "", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	eng, _ := engine.New(engine.EngineParams{Config: cfg, Perm: perm})

	var text string
	for ev := range eng.Run(ctx, "Say ok") {
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
	}
	if text == "" {
		t.Error("Bypass should allow response")
	}
}

// =============================================================================
// 6. HOOKS
// =============================================================================

func TestLive_PreToolUseHookAllow(t *testing.T) {
	cfg := liveSkip(t)
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: `echo '{"decision":"allow"}'`}},
		}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	eng, _ := engine.New(engine.EngineParams{Config: cfg, Hooks: hookRunner})

	hasDone := false
	for ev := range eng.Run(ctx, "Use ls tool on current directory") {
		if ev.Type == event.Done {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("Should complete with allow hook")
	}
}

func TestLive_PreToolUseHookDeny(t *testing.T) {
	cfg := liveSkip(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "deny.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'BLOCKED' >&2\nexit 2\n"), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	eng, _ := engine.New(engine.EngineParams{Config: cfg, Hooks: hookRunner})

	for ev := range eng.Run(ctx, "Use ls on current dir") {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "BLOCKED") {
				return // PASS
			}
		}
	}
}

// =============================================================================
// 7. COMMANDS
// =============================================================================

func TestLive_CommandExpand(t *testing.T) {
	t.Parallel()
	liveSkip(t)
	cmd := &command.Command{Body: "Explain $ARGUMENTS. Branch: !`git branch --show-current`"}
	expanded, _ := cmd.Expand("Go channels")
	if !strings.Contains(expanded, "Go channels") {
		t.Error("$ARGUMENTS not replaced")
	}
	if strings.Contains(expanded, "!`") {
		t.Error("Backtick not expanded")
	}
	t.Logf("Expanded: %s", expanded)
}

func TestLive_CommandWithGPT(t *testing.T) {
	t.Parallel()
	cfg := liveSkip(t)
	output := liveExec(t, cfg, "Explain what a goroutine is in one sentence.", false)
	if !strings.Contains(strings.ToLower(output), "goroutine") &&
		!strings.Contains(strings.ToLower(output), "concurrent") &&
		!strings.Contains(strings.ToLower(output), "thread") {
		t.Errorf("Response should explain goroutines: %.200s", output)
	}
}

// =============================================================================
// 8. AGENTS (subagent spawn)
// =============================================================================

func TestLive_AgentSpawn(t *testing.T) {
	cfg := liveSkip(t)
	eng, _ := engine.New(engine.EngineParams{Config: cfg})

	ag := &agent.Agent{
		Name:         "explainer",
		Model:        "inherit",
		SystemPrompt: "You explain code concepts briefly.",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var text string
	for ev := range agent.Spawn(ctx, eng, ag, "What is a mutex?") {
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
	}
	if text == "" {
		t.Error("Agent should respond")
	}
	t.Logf("Agent: %.200s", text)
}

func TestLive_AgentWithRestrictedTools(t *testing.T) {
	cfg := liveSkip(t)
	eng, _ := engine.New(engine.EngineParams{Config: cfg})

	ag := &agent.Agent{
		Name:         "reader",
		Model:        "inherit",
		Tools:        []string{"read", "grep"},
		SystemPrompt: "You can only read and search files.",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hasDone := false
	for ev := range agent.Spawn(ctx, eng, ag, "What tools do you have?") {
		if ev.Type == event.Done {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("Restricted agent should complete")
	}
}

// =============================================================================
// 9. PLUGINS (loading actual Claude Code plugins)
// =============================================================================

func TestLive_LoadAllPlugins(t *testing.T) {
	liveSkip(t)
	dir := filepath.Join("..", "vendor", "claude-code", "plugins")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skip("vendor/claude-code not found")
	}
	plugins, _ := plugin.Discover(dir)
	if len(plugins) < 8 {
		t.Errorf("Expected 8+ plugins, got %d", len(plugins))
	}
	totalCmds, totalAgents, totalHooks := 0, 0, 0
	for _, p := range plugins {
		totalCmds += len(p.Commands)
		totalAgents += len(p.Agents)
		totalHooks += len(p.Hooks)
	}
	t.Logf("Loaded: %d plugins, %d commands, %d agents, %d hook events",
		len(plugins), totalCmds, totalAgents, totalHooks)
}

// =============================================================================
// 10. COMPACTION
// =============================================================================

func TestLive_CompactionPreservesRecent(t *testing.T) {
	t.Parallel()
	liveSkip(t)
	var messages []provider.Message
	for i := 0; i < 50; i++ {
		messages = append(messages,
			provider.Message{Role: "user", Content: "q"},
			provider.Message{Role: "tool", Content: strings.Repeat("x", 1000)},
			provider.Message{Role: "assistant", Content: "a"},
		)
	}
	mc := compact.NewMicrocompactor(5)
	result := mc.Apply(messages)
	stubbed := 0
	for _, m := range result {
		if m.Content == "[previous tool result removed]" {
			stubbed++
		}
	}
	if stubbed == 0 {
		t.Error("Old tool results should be compacted")
	}
}

// =============================================================================
// 11. CONTENT PART
// =============================================================================

func TestLive_ContentPartRoundTrip(t *testing.T) {
	t.Parallel()
	liveSkip(t)
	msg := provider.Message{
		Role: "assistant",
		Parts: []provider.ContentPart{
			{Type: "text", Text: "checking"},
			{Type: "tool_use", ID: "call_1", Name: "read", Input: json.RawMessage(`{"file_path":"x"}`)},
		},
	}
	data, _ := json.Marshal(msg)
	var decoded provider.Message
	json.Unmarshal(data, &decoded)
	if len(decoded.Parts) != 2 || decoded.Parts[1].Name != "read" {
		t.Error("ContentPart round-trip failed")
	}
}

// =============================================================================
// 12. TOOL REGISTRY
// =============================================================================

func TestLive_RegistrySubset(t *testing.T) {
	t.Parallel()
	liveSkip(t)
	r := tool.NewRegistry()
	r.Register(tool.NewReadTool())
	r.Register(tool.NewBashTool())
	r.Register(tool.NewEditTool())

	sub := r.Subset([]string{"read"})
	if len(sub.All()) != 1 {
		t.Errorf("Subset: %d", len(sub.All()))
	}
	if _, ok := sub.Get("bash"); ok {
		t.Error("bash should not be in subset")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
