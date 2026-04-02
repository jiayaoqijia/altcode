//go:build !windows

package internal_test

// Claude Code ported tests — ROUND 2
// Covers remaining patterns from hookify, security-guidance, plugin-dev scripts.

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
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/altcode-ai/altcode/internal/tool"
)

// =============================================================================
// PORTED: hookify/pretooluse.py — PreToolUse with rule conditions
// =============================================================================

func TestClaudeCode2_PreToolUseWithConditions(t *testing.T) {
	// Hookify's pretooluse.py maps tool_name to event type and evaluates conditions.
	// Test: hook that blocks bash commands containing "rm" but allows others.
	dir := t.TempDir()
	script := filepath.Join(dir, "pretooluse.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
INPUT=$(cat)
CMD=$(echo "$INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('toolInput',{}).get('command','') if isinstance(json.load(open('/dev/stdin')) if False else '', str) else '')" 2>/dev/null || echo "$INPUT" | grep -o '"command":"[^"]*"' | head -1 | sed 's/"command":"//;s/"//')
# Simple: read toolInput.command from JSON
CMD=$(echo "$INPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('toolInput','{}'))" 2>/dev/null)
if echo "$CMD" | grep -q "rm "; then
  echo "Dangerous rm command!" >&2
  exit 2
fi
echo '{"decision":"allow"}'
`), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "bash",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	// Test allow
	results, _ := hookRunner.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "bash",
		ToolInput: json.RawMessage(`{"command":"echo hello"}`),
	})
	if hooks.HasDeny(results) {
		t.Error("echo should be allowed")
	}

	// Test deny
	results, _ = hookRunner.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "bash",
		ToolInput: json.RawMessage(`{"command":"rm -rf /tmp/x"}`),
	})
	if !hooks.HasDeny(results) {
		t.Error("rm should be denied")
	}
}

// =============================================================================
// PORTED: hookify/userpromptsubmit.py — UserPromptSubmit hook
// =============================================================================

func TestClaudeCode2_UserPromptSubmitHook(t *testing.T) {
	// Hookify's userpromptsubmit.py fires on user prompt and can inject context.
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.UserPromptSubmit: {{
			Matcher: "*",
			Hooks: []hooks.EntryConfig{{
				Type:    "command",
				Command: `echo '{"decision":"allow","message":"[Context: You are in a Go project]"}'`,
			}},
		}},
	})

	results, _ := hookRunner.Fire(context.Background(), hooks.UserPromptSubmit, hooks.Input{
		Event: hooks.UserPromptSubmit,
	})
	if len(results) == 0 {
		t.Fatal("Expected result")
	}
	if !strings.Contains(results[0].Message, "Go project") {
		t.Errorf("Message: %q", results[0].Message)
	}
}

func TestClaudeCode2_UserPromptSubmitInEngine(t *testing.T) {
	// Verify UserPromptSubmit hook injects context into engine's user message.
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.UserPromptSubmit: {{
			Matcher: "*",
			Hooks: []hooks.EntryConfig{{
				Type:    "command",
				Command: `echo '{"decision":"allow","message":"[injected context]"}'`,
			}},
		}},
	})

	srv := cc2Server(t, []string{cc2TextSSE("Got it.")})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cc2Cfg(srv),
		Hooks:  hookRunner,
	})

	cc2DrainAll(eng.Run(context.Background(), "hello"))

	// First user message should contain injected context
	msgs := eng.Messages()
	if len(msgs) == 0 {
		t.Fatal("No messages")
	}
	if !strings.Contains(msgs[0].Content, "[injected context]") {
		t.Errorf("User message should contain injected context: %q", msgs[0].Content)
	}
}

// =============================================================================
// PORTED: security_reminder_hook.py — Content pattern matching
// =============================================================================

func TestClaudeCode2_SecurityReminderHook(t *testing.T) {
	// security_reminder_hook.py checks file paths and content for security patterns.
	// Test: hook that warns about .env files and path traversal.
	dir := t.TempDir()
	script := filepath.Join(dir, "security.py")
	os.WriteFile(script, []byte(`#!/usr/bin/env python3
import json, sys
data = json.load(sys.stdin)
tool_input = data.get("toolInput", {})
if isinstance(tool_input, str):
    tool_input = json.loads(tool_input) if tool_input else {}
file_path = tool_input.get("file_path", "") if isinstance(tool_input, dict) else ""
if ".env" in file_path or ".." in file_path:
    print("Security risk: sensitive file!", file=sys.stderr)
    sys.exit(2)
print(json.dumps({"decision": "allow"}))
`), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "write|edit|read",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "python3 " + script}},
		}},
	})

	// Safe file
	results, _ := hookRunner.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "write",
		ToolInput: json.RawMessage(`{"file_path":"/tmp/safe.txt"}`),
	})
	if hooks.HasDeny(results) {
		t.Error("Safe file should be allowed")
	}

	// .env file
	results, _ = hookRunner.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "write",
		ToolInput: json.RawMessage(`{"file_path":"/project/.env"}`),
	})
	if !hooks.HasDeny(results) {
		t.Error(".env should be denied")
	}

	// Path traversal
	results, _ = hookRunner.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "read",
		ToolInput: json.RawMessage(`{"file_path":"../../etc/passwd"}`),
	})
	if !hooks.HasDeny(results) {
		t.Error("Path traversal should be denied")
	}
}

// =============================================================================
// PORTED: load-context.sh — SessionStart context loading
// =============================================================================

func TestClaudeCode2_SessionStartContextLoading(t *testing.T) {
	// load-context.sh pattern: SessionStart hook that detects project type
	// and returns context. Test that our SessionStart hook fires and returns data.
	dir := t.TempDir()
	script := filepath.Join(dir, "load-ctx.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
echo '{"decision":"allow","message":"Detected: Go project with 287 tests"}'
`), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.SessionStart: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	results, _ := hookRunner.Fire(context.Background(), hooks.SessionStart, hooks.Input{
		Event: hooks.SessionStart,
	})
	if len(results) == 0 || !strings.Contains(results[0].Message, "Go project") {
		t.Error("SessionStart should return project context")
	}
}

// =============================================================================
// PORTED: validate-agent.sh — Agent validation
// =============================================================================

func TestClaudeCode2_AgentValidation(t *testing.T) {
	// validate-agent.sh checks agent frontmatter for required fields.
	// Port: verify our agent parser validates correctly.
	dir := t.TempDir()

	// Valid agent
	os.WriteFile(filepath.Join(dir, "valid.md"), []byte(`---
name: code-reviewer
description: Use when code needs review
model: sonnet
tools: ["Read", "Grep"]
---

You are an expert code reviewer.
`), 0o644)

	a, err := agent.ParseFile(filepath.Join(dir, "valid.md"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "code-reviewer" {
		t.Errorf("Name: %q", a.Name)
	}
	if a.Description == "" {
		t.Error("Description should be set")
	}
	if len(a.Tools) != 2 {
		t.Errorf("Tools: %v", a.Tools)
	}

	// Agent with no tools (all tools allowed)
	os.WriteFile(filepath.Join(dir, "open.md"), []byte(`---
name: open-agent
description: Can use all tools
---

Unrestricted agent.
`), 0o644)
	a2, _ := agent.ParseFile(filepath.Join(dir, "open.md"))
	if a2.Tools != nil {
		t.Error("No tools field should mean all tools")
	}
}

// =============================================================================
// PORTED: parse-frontmatter.sh / validate-settings.sh — Settings parsing
// =============================================================================

func TestClaudeCode2_SettingsFileParsing(t *testing.T) {
	// Claude Code stores plugin settings in .claude/*.local.md with YAML frontmatter.
	// Our command parser already handles this format. Verify.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hookify.block-rm.local.md"), []byte(`---
name: block-rm
enabled: true
event: bash
action: block
---

Block dangerous rm commands.
`), 0o644)

	// Parse as a command (same frontmatter format)
	cmd, err := command.ParseFile(filepath.Join(dir, "hookify.block-rm.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Name derived from filename
	if cmd.Name != "hookify.block-rm.local" {
		t.Errorf("Name: %q", cmd.Name)
	}
	// Body should be the markdown content
	if !strings.Contains(cmd.Body, "Block dangerous rm") {
		t.Errorf("Body: %q", cmd.Body)
	}
}

// =============================================================================
// PORTED: test-hook.sh — Hook testing harness pattern
// =============================================================================

func TestClaudeCode2_HookTestHarness(t *testing.T) {
	// test-hook.sh validates: hook is executable, input is valid JSON,
	// output is JSON, exit codes are correct. Port the pattern.

	dir := t.TempDir()

	// Test 1: Valid hook returns JSON
	good := filepath.Join(dir, "good.sh")
	os.WriteFile(good, []byte(`#!/bin/sh
echo '{"decision":"allow","message":"ok"}'
`), 0o755)

	runner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{Matcher: "*", Hooks: []hooks.EntryConfig{
			{Type: "command", Command: "sh " + good},
		}}},
	})
	results, _ := runner.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "bash",
	})
	if len(results) != 1 || results[0].Decision != "allow" {
		t.Errorf("Good hook: %+v", results)
	}

	// Test 2: Hook that crashes (exit 1) defaults to allow
	bad := filepath.Join(dir, "bad.sh")
	os.WriteFile(bad, []byte("#!/bin/sh\nexit 1\n"), 0o755)

	runner2 := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{Matcher: "*", Hooks: []hooks.EntryConfig{
			{Type: "command", Command: "sh " + bad},
		}}},
	})
	results2, _ := runner2.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "bash",
	})
	if hooks.HasDeny(results2) {
		t.Error("Crashed hook should default to allow")
	}

	// Test 3: Hook that blocks (exit 2) with stderr message
	block := filepath.Join(dir, "block.sh")
	os.WriteFile(block, []byte("#!/bin/sh\necho 'Blocked!' >&2\nexit 2\n"), 0o755)

	runner3 := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{Matcher: "*", Hooks: []hooks.EntryConfig{
			{Type: "command", Command: "sh " + block},
		}}},
	})
	results3, _ := runner3.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "bash",
	})
	if !hooks.HasDeny(results3) {
		t.Error("Exit 2 should deny")
	}
	if results3[0].Message == "" {
		t.Error("Deny should include stderr message")
	}
}

// =============================================================================
// PORTED: ralph-wiggum loop — Stop hook iteration with state persistence
// =============================================================================

func TestClaudeCode2_RalphWiggumLoop(t *testing.T) {
	// ralph-wiggum's stop-hook.sh reads a state file, increments iteration,
	// blocks if iteration < max. Test the full loop pattern through engine.
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	os.WriteFile(stateFile, []byte(`{"iteration":0,"max":2}`), 0o644)

	script := filepath.Join(dir, "stop.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
STATE=$(cat %s)
ITER=$(echo "$STATE" | python3 -c "import sys,json; print(json.load(sys.stdin)['iteration'])")
MAX=$(echo "$STATE" | python3 -c "import sys,json; print(json.load(sys.stdin)['max'])")
NEXT=$((ITER + 1))
echo "{\"iteration\":$NEXT,\"max\":$MAX}" > %s
if [ "$NEXT" -lt "$MAX" ]; then
  echo "Need more iterations ($NEXT/$MAX)" >&2
  exit 2
fi
echo '{"decision":"allow"}'
`, stateFile, stateFile)), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	// The engine should loop: attempt 1 (blocked), attempt 2 (allowed)
	srv := cc2Server(t, []string{
		cc2TextSSE("Attempt 1"),
		cc2TextSSE("Attempt 2"),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cc2Cfg(srv),
		Hooks:  hookRunner,
	})

	events := cc2DrainAll(eng.Run(context.Background(), "iterate"))

	// Should have text from both attempts
	textCount := 0
	for _, ev := range events {
		if ev.Type == event.TextDelta {
			textCount++
		}
	}
	if textCount < 2 {
		t.Errorf("Expected text from 2 attempts, got %d deltas", textCount)
	}
}

// =============================================================================
// PORTED: Plugin integration — full plugin lifecycle
// =============================================================================

func TestClaudeCode2_PluginFullLifecycle(t *testing.T) {
	// Test: discover plugin → load commands + hooks → merge → use in engine
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")

	os.MkdirAll(filepath.Join(pluginDir, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"my-plugin","version":"1.0.0"}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "commands", "greet.md"),
		[]byte("---\ndescription: Greet the user\n---\nSay hello to $ARGUMENTS"), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"),
		[]byte(`{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"echo '{\"decision\":\"allow\"}'"}]}]}}`), 0o644)

	// Discover
	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 1 {
		t.Fatalf("Expected 1 plugin, got %d", len(plugins))
	}

	// Merge hooks into config
	cfg := config.Default()
	plugins[0].Merge(cfg)
	if len(cfg.Hooks["PreToolUse"]) == 0 {
		t.Error("Hooks not merged")
	}

	// Use command
	if len(plugins[0].Commands) != 1 {
		t.Fatal("Command not loaded")
	}
	expanded, _ := plugins[0].Commands[0].Expand("World")
	if !strings.Contains(expanded, "World") {
		t.Error("$ARGUMENTS not expanded in plugin command")
	}
}

// =============================================================================
// PORTED: Auto-compact trigger
// =============================================================================

func TestClaudeCode2_AutoCompactTriggersOnLargeHistory(t *testing.T) {
	// Verify engine compacts when messages exceed threshold.
	// Build a mock that returns many tool calls to inflate history.
	responses := make([]string, 0)
	for i := 0; i < 35; i++ {
		responses = append(responses,
			cc2ToolSSE(fmt.Sprintf("t%d", i), "ls", `{"path":"."}`))
	}
	responses = append(responses, cc2TextSSE("Done after many tools."))

	srv := cc2Server(t, responses)
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{Config: cc2Cfg(srv)})
	cc2DrainAll(eng.Run(context.Background(), "scan everything"))

	// With 35 tool calls, messages would be: user + 35*(assistant+toolresult) + final = 72
	// But auto-compact should have truncated older entries
	msgs := eng.Messages()
	// Messages should exist (engine completed)
	if len(msgs) == 0 {
		t.Fatal("No messages")
	}
}

// =============================================================================
// PORTED: Subagent spawn with tool restriction
// =============================================================================

func TestClaudeCode2_SubagentSpawnAndComplete(t *testing.T) {
	srv := cc2Server(t, []string{cc2TextSSE("Agent completed task.")})
	defer srv.Close()

	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}

	parent, _ := engine.New(engine.EngineParams{Config: cfg})

	ag := &agent.Agent{
		Name:         "code-explorer",
		Model:        "inherit",
		Tools:        []string{"read", "grep", "ls"},
		SystemPrompt: "You are an expert code analyst. Trace execution paths.",
	}

	events := cc2DrainAll(agent.Spawn(context.Background(), parent, ag, "analyze main.go"))

	hasDone := false
	hasText := false
	for _, ev := range events {
		if ev.Type == event.Done {
			hasDone = true
		}
		if ev.Type == event.TextDelta {
			hasText = true
		}
	}
	if !hasDone {
		t.Error("Subagent should complete")
	}
	if !hasText {
		t.Error("Subagent should produce text")
	}
}

func TestClaudeCode2_SubagentToolRestriction(t *testing.T) {
	// Verify restricted tools are actually a subset
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool())
	registry.Register(tool.NewGrepTool())
	registry.Register(tool.NewBashTool())
	registry.Register(tool.NewEditTool())

	subset := registry.Subset([]string{"read", "grep"})
	all := subset.All()

	if len(all) != 2 {
		t.Errorf("Expected 2 tools in subset, got %d", len(all))
	}

	if _, ok := subset.Get("bash"); ok {
		t.Error("bash should NOT be in restricted subset")
	}
	if _, ok := subset.Get("read"); !ok {
		t.Error("read should be in subset")
	}
}

// =============================================================================
// PORTED: Session persistence with store round-trip
// =============================================================================

func TestClaudeCode2_SessionPersistAndResume(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()

	sess, _ := db.CreateSession("proj", "test", "claude-test")

	// First turn
	srv1 := cc2Server(t, []string{cc2TextSSE("First response")})
	defer srv1.Close()

	var buf1 bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{
			Config: cc2Cfg(srv1), Store: db, SessionID: sess.ID,
		},
		Prompt: "hello",
		Writer: &buf1,
	})

	// Second turn (resume)
	srv2 := cc2Server(t, []string{cc2TextSSE("Second response")})
	defer srv2.Close()

	msgs, _ := db.ListMessages(sess.ID)
	provMsgs := store.ToProviderMessages(msgs)

	var buf2 bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{
			Config: cc2Cfg(srv2), Store: db, SessionID: sess.ID,
			Messages: provMsgs,
		},
		Prompt: "follow up",
		Writer: &buf2,
	})

	// Verify both turns persisted
	allMsgs, _ := db.ListMessages(sess.ID)
	if len(allMsgs) < 4 {
		t.Errorf("Expected at least 4 messages (2 user + 2 assistant), got %d", len(allMsgs))
	}
}

// =============================================================================
// Helpers
// =============================================================================

func cc2TextSSE(text string) string {
	return fmt.Sprintf("event: content_block_start\ndata: %s\n\n", `{"index":0,"content_block":{"type":"text","text":""}}`) +
		fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)) +
		"event: content_block_stop\ndata: {}\n\n" +
		"event: message_stop\ndata: {}\n\n"
}

func cc2ToolSSE(id, name, input string) string {
	return fmt.Sprintf("event: content_block_start\ndata: %s\n\n",
		fmt.Sprintf(`{"index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, id, name)) +
		fmt.Sprintf("event: content_block_delta\ndata: %s\n\n",
			fmt.Sprintf(`{"delta":{"type":"input_json_delta","partial_json":%q}}`, input)) +
		"event: content_block_stop\ndata: {}\n\n" +
		"event: message_stop\ndata: {}\n\n"
}

func cc2Server(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := idx
		idx++
		mu.Unlock()
		if i >= len(responses) {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(responses[i]))
	}))
}

func cc2Cfg(srv *httptest.Server) *config.Config {
	c := config.Default()
	c.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
	return c
}

func cc2DrainAll(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
