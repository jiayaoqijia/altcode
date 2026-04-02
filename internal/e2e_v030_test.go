//go:build !windows

package internal_test

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
	"time"

	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/plugin"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/store"
)

// =============================================================================
// HELPERS (shared across all e2e tests)
// =============================================================================

func sseEv(eventType, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)
}

func textSSE030(text string) string {
	return sseEv("content_block_start", `{"index":0,"content_block":{"type":"text","text":""}}`) +
		sseEv("content_block_delta", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)) +
		sseEv("content_block_stop", `{}`) +
		sseEv("message_stop", `{}`)
}

func toolSSE030(id, name, inputJSON string) string {
	return sseEv("content_block_start", fmt.Sprintf(
		`{"index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, id, name)) +
		sseEv("content_block_delta", fmt.Sprintf(
			`{"delta":{"type":"input_json_delta","partial_json":%q}}`, inputJSON)) +
		sseEv("content_block_stop", `{}`) +
		sseEv("message_stop", `{}`)
}

func multiCallServer(t *testing.T, responses []string) *httptest.Server {
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

func cfgForServer(srv *httptest.Server) *config.Config {
	c := config.Default()
	c.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
	return c
}

func drainEvents(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func countEv(events []event.Event, t event.EventType) int {
	n := 0
	for _, e := range events {
		if e.Type == t {
			n++
		}
	}
	return n
}

// =============================================================================
// 1. EXEC MODE E2E
// =============================================================================

func TestE2E_ExecTextMode(t *testing.T) {
	srv := multiCallServer(t, []string{textSSE030("Hello exec!")})
	defer srv.Close()
	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgForServer(srv)},
		Prompt:       "hi",
		Writer:       &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Hello exec!") {
		t.Errorf("output: %q", buf.String())
	}
}

func TestE2E_ExecJSONMode(t *testing.T) {
	srv := multiCallServer(t, []string{textSSE030("json out")})
	defer srv.Close()
	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgForServer(srv)},
		Prompt:       "hi",
		JSON:         true,
		Writer:       &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var foundDone bool
	for _, line := range lines {
		var ev event.Event
		if json.Unmarshal([]byte(line), &ev) == nil && ev.Type == event.Done {
			foundDone = true
		}
	}
	if !foundDone {
		t.Error("JSON output missing Done event")
	}
}

func TestE2E_ExecWithToolCall(t *testing.T) {
	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		textSSE030("Listed."),
	})
	defer srv.Close()
	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgForServer(srv)},
		Prompt:       "list files",
		Writer:       &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Listed.") {
		t.Errorf("Expected final text, got: %q", buf.String())
	}
}

func TestE2E_ExecErrorReturnsNonNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"fail"}`))
	}))
	defer srv.Close()
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgForServer(srv)},
		Prompt:       "hi",
		Writer:       &bytes.Buffer{},
	})
	if err == nil {
		t.Error("Expected non-nil error for 500")
	}
}

func TestE2E_ExecEmptyPrompt(t *testing.T) {
	srv := multiCallServer(t, []string{textSSE030("OK")})
	defer srv.Close()
	var buf bytes.Buffer
	// Empty prompt should still work
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgForServer(srv)},
		Prompt:       "",
		Writer:       &buf,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// =============================================================================
// 2. SESSION RESUME ROUND-TRIP
// =============================================================================

func TestE2E_SessionCreatePersistReload(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()

	sess, _ := db.CreateSession("proj", "test", "claude-test")

	srv := multiCallServer(t, []string{textSSE030("First response")})
	defer srv.Close()

	// Run first turn
	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{
			Config: cfgForServer(srv), Store: db, SessionID: sess.ID,
		},
		Prompt: "hello",
		Writer: &buf,
	})

	// Verify persisted
	msgs, _ := db.ListMessages(sess.ID)
	if len(msgs) < 2 {
		t.Fatalf("Expected >= 2 persisted messages, got %d", len(msgs))
	}

	// Reload into provider messages
	provMsgs := store.ToProviderMessages(msgs)
	if len(provMsgs) < 2 {
		t.Fatal("ToProviderMessages failed")
	}
	if provMsgs[0].Role != "user" || provMsgs[0].Content != "hello" {
		t.Errorf("First msg wrong: %+v", provMsgs[0])
	}
}

func TestE2E_SessionResumeWithPreloadedHistory(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()

	sess, _ := db.CreateSession("proj", "resume-test", "claude-test")

	// Simulate a previous turn
	userMsg := provider.TextMessage("user", "previous question")
	data, _ := json.Marshal(userMsg)
	db.AddMessage(sess.ID, "user", data, "claude-test", 10, 0)
	assistMsg := provider.TextMessage("assistant", "previous answer")
	data, _ = json.Marshal(assistMsg)
	db.AddMessage(sess.ID, "assistant", data, "claude-test", 0, 20)

	// Load and verify
	msgs, _ := db.ListMessages(sess.ID)
	provMsgs := store.ToProviderMessages(msgs)

	srv := multiCallServer(t, []string{textSSE030("Continued!")})
	defer srv.Close()

	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{
			Config: cfgForServer(srv), Store: db, SessionID: sess.ID,
			Messages: provMsgs,
		},
		Prompt: "follow up",
		Writer: &buf,
	})

	if !strings.Contains(buf.String(), "Continued!") {
		t.Errorf("Expected resumed response, got: %q", buf.String())
	}
}

func TestE2E_SessionResumeWithToolUseParts(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()

	sess, _ := db.CreateSession("proj", "tools-resume", "claude-test")

	// Save a message with ContentParts (tool_use)
	toolMsg := provider.Message{
		Role: "assistant",
		Parts: []provider.ContentPart{
			{Type: "text", Text: "I'll read that."},
			{Type: "tool_use", ID: "t1", Name: "read", Input: json.RawMessage(`{"file_path":"/tmp/x"}`)},
		},
	}
	data, _ := json.Marshal(toolMsg)
	db.AddMessage(sess.ID, "assistant", data, "claude-test", 0, 50)

	// Reload
	msgs, _ := db.ListMessages(sess.ID)
	provMsgs := store.ToProviderMessages(msgs)

	if len(provMsgs) == 0 {
		t.Fatal("No messages loaded")
	}
	if len(provMsgs[0].Parts) != 2 {
		t.Fatalf("Parts lost in round-trip: %d", len(provMsgs[0].Parts))
	}
	if provMsgs[0].Parts[1].Name != "read" {
		t.Errorf("Tool name lost: %q", provMsgs[0].Parts[1].Name)
	}
}

func TestE2E_LatestSessionQuery(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()

	db.CreateSession("proj", "old", "model-a")
	time.Sleep(10 * time.Millisecond)
	s2, _ := db.CreateSession("proj", "new", "model-b")

	latest, err := db.LatestSession("proj")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != s2.ID {
		t.Errorf("Latest should be %q, got %q", s2.ID, latest.ID)
	}
}

func TestE2E_LatestSessionEmpty(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()

	_, err := db.LatestSession("nonexistent")
	if err == nil {
		t.Error("Expected error for no sessions")
	}
}

// =============================================================================
// 3. HOOKS BLOCKING TOOL EXECUTION IN AGENT LOOP
// =============================================================================

func TestE2E_HookDeniesToolCall(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "deny.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'DANGEROUS!' >&2\nexit 2\n"), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "bash",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	srv := multiCallServer(t, []string{
		toolSSE030("t1", "bash", `{"command":"rm -rf /"}`),
		textSSE030("OK, won't do that."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "delete everything"))

	// Tool result should show hook blocked it
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "DANGEROUS!") {
				return // PASS
			}
		}
	}
	t.Error("Expected hook deny message in tool result")
}

func TestE2E_HookAllowsToolCall(t *testing.T) {
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "ls",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: `echo '{"decision":"allow"}'`}},
		}},
	})

	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		textSSE030("Listed."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "list"))

	// Should NOT be blocked
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Blocked") {
				t.Error("Hook should have allowed")
			}
		}
	}
	if countEv(events, event.Done) != 1 {
		t.Error("Expected Done")
	}
}

func TestE2E_HookOnlyMatchesTargetTool(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "deny.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nexit 2\n"), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "bash", // only matches bash, not ls
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`), // ls, not bash
		textSSE030("Listed."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "list"))

	// ls should NOT be blocked by bash hook
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Blocked") {
				t.Error("Hook for bash should not block ls")
			}
		}
	}
}

// =============================================================================
// 4. STOP HOOKS
// =============================================================================

func TestE2E_StopHookBlocksCompletion(t *testing.T) {
	// Stop hook denies first completion, model gets a second chance
	dir := t.TempDir()
	callCount := filepath.Join(dir, "count")
	os.WriteFile(callCount, []byte("0"), 0o644)

	script := filepath.Join(dir, "stop.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
COUNT=$(cat %s)
if [ "$COUNT" = "0" ]; then
  echo "1" > %s
  echo "Tests not run!" >&2
  exit 2
fi
echo '{"decision":"allow"}'
`, callCount, callCount)), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	// First response blocked by stop hook → loop continues → second response allowed
	srv := multiCallServer(t, []string{
		textSSE030("First attempt."),
		textSSE030("Second attempt with tests."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "implement feature"))

	// Should have two text responses
	textCount := 0
	for _, ev := range events {
		if ev.Type == event.TextDelta {
			textCount++
		}
	}
	if textCount < 2 {
		t.Errorf("Expected text from both attempts, got %d deltas", textCount)
	}

	// Messages should include the stop hook's block reason
	msgs := eng.Messages()
	foundBlock := false
	for _, m := range msgs {
		if m.Role == "user" && strings.Contains(m.Content, "Tests not run!") {
			foundBlock = true
		}
	}
	if !foundBlock {
		t.Error("Stop hook block reason should be injected as user message")
	}
}

func TestE2E_StopHookAllowsCompletion(t *testing.T) {
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: `echo '{"decision":"allow"}'`}},
		}},
	})

	srv := multiCallServer(t, []string{textSSE030("Done.")})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "hi"))

	if countEv(events, event.Done) != 1 {
		t.Error("Stop hook with allow should let completion proceed")
	}
}

// =============================================================================
// 5. SLASH COMMAND EXPANSION
// =============================================================================

func TestE2E_CommandExpandArguments(t *testing.T) {
	cmd := &command.Command{Body: "Analyze $ARGUMENTS for bugs."}
	result, _ := cmd.Expand("main.go")
	if result != "Analyze main.go for bugs." {
		t.Errorf("Got: %q", result)
	}
}

func TestE2E_CommandExpandBacktick(t *testing.T) {
	cmd := &command.Command{Body: "Version: !`echo v1.0`"}
	result, _ := cmd.Expand("")
	if !strings.Contains(result, "v1.0") {
		t.Errorf("Backtick not expanded: %q", result)
	}
}

func TestE2E_CommandExpandMultipleBackticks(t *testing.T) {
	cmd := &command.Command{Body: "A: !`echo one` B: !`echo two`"}
	result, _ := cmd.Expand("")
	if !strings.Contains(result, "one") || !strings.Contains(result, "two") {
		t.Errorf("Multiple backticks not expanded: %q", result)
	}
}

func TestE2E_CommandWithAllowedTools(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "safe.md"), []byte(`---
description: Safe command
allowed-tools: Read, Grep
---
Only read and grep.
`), 0o644)

	cmds, _ := command.Discover(dir)
	if len(cmds) != 1 {
		t.Fatal("Expected 1 command")
	}
	if len(cmds[0].AllowedTools) != 2 {
		t.Errorf("Expected 2 allowed tools, got %v", cmds[0].AllowedTools)
	}
	if cmds[0].AllowedTools[0] != "Read" || cmds[0].AllowedTools[1] != "Grep" {
		t.Errorf("Wrong tools: %v", cmds[0].AllowedTools)
	}
}

func TestE2E_CommandFrontmatterEdgeCases(t *testing.T) {
	dir := t.TempDir()

	// Only opening --- (no closing)
	os.WriteFile(filepath.Join(dir, "broken.md"), []byte("---\nno closing\n"), 0o644)
	cmd, _ := command.ParseFile(filepath.Join(dir, "broken.md"))
	if cmd.Description != "" {
		t.Error("Broken frontmatter should be treated as body")
	}

	// Empty frontmatter
	os.WriteFile(filepath.Join(dir, "empty.md"), []byte("---\n---\nBody here."), 0o644)
	cmd, _ = command.ParseFile(filepath.Join(dir, "empty.md"))
	if cmd.Body != "Body here." {
		t.Errorf("Body wrong: %q", cmd.Body)
	}
}

// =============================================================================
// 6. PLUGIN DISCOVERY + MERGE INTO ENGINE
// =============================================================================

func TestE2E_PluginDiscoverAndMerge(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")

	// Create plugin structure
	os.MkdirAll(filepath.Join(pluginDir, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"my-plugin","version":"1.0.0"}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "commands", "deploy.md"),
		[]byte("---\ndescription: Deploy app\n---\nDeploy the application."), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"),
		[]byte(`{"hooks":{"PreToolUse":[{"matcher":"bash","hooks":[{"type":"command","command":"echo ok"}]}]}}`), 0o644)

	// Discover
	plugins, err := plugin.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 {
		t.Fatalf("Expected 1 plugin, got %d", len(plugins))
	}

	// Merge into config
	cfg := config.Default()
	for _, p := range plugins {
		p.Merge(cfg)
	}

	if len(cfg.Hooks["PreToolUse"]) != 1 {
		t.Errorf("Expected merged PreToolUse hook, got %d", len(cfg.Hooks["PreToolUse"]))
	}

	// Verify commands
	if len(plugins[0].Commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(plugins[0].Commands))
	}
	if plugins[0].Commands[0].Name != "deploy" {
		t.Errorf("Command name: %q", plugins[0].Commands[0].Name)
	}
}

func TestE2E_PluginInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "bad-plugin")
	os.MkdirAll(filepath.Join(pluginDir, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".altcode-plugin", "plugin.json"),
		[]byte(`not json`), 0o644)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 0 {
		t.Error("Should skip invalid manifest")
	}
}

func TestE2E_PluginNoManifest(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "not-a-plugin"), 0o755)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 0 {
		t.Error("Should skip directories without plugin.json")
	}
}

func TestE2E_MultiplePluginsMerge(t *testing.T) {
	dir := t.TempDir()

	// Plugin A with PreToolUse hook
	pA := filepath.Join(dir, "plugin-a")
	os.MkdirAll(filepath.Join(pA, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pA, ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-a"}`), 0o644)
	os.MkdirAll(filepath.Join(pA, "hooks"), 0o755)
	os.WriteFile(filepath.Join(pA, "hooks", "hooks.json"),
		[]byte(`{"hooks":{"PreToolUse":[{"matcher":"bash","hooks":[{"type":"command","command":"echo a"}]}]}}`), 0o644)

	// Plugin B with Stop hook
	pB := filepath.Join(dir, "plugin-b")
	os.MkdirAll(filepath.Join(pB, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pB, ".altcode-plugin", "plugin.json"),
		[]byte(`{"name":"plugin-b"}`), 0o644)
	os.MkdirAll(filepath.Join(pB, "hooks"), 0o755)
	os.WriteFile(filepath.Join(pB, "hooks", "hooks.json"),
		[]byte(`{"hooks":{"Stop":[{"matcher":"*","hooks":[{"type":"command","command":"echo b"}]}]}}`), 0o644)

	plugins, _ := plugin.Discover(dir)
	cfg := config.Default()
	for _, p := range plugins {
		p.Merge(cfg)
	}

	if len(cfg.Hooks["PreToolUse"]) < 1 {
		t.Error("Missing PreToolUse from plugin-a")
	}
	if len(cfg.Hooks["Stop"]) < 1 {
		t.Error("Missing Stop from plugin-b")
	}
}

// =============================================================================
// 7. MULTI-TURN AGENT LOOP WITH HOOKS ON EACH CALL
// =============================================================================

func TestE2E_HookFiresOnEachToolCall(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "hook.log")

	script := filepath.Join(dir, "log.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
echo "fired" >> %s
echo '{"decision":"allow"}'
`, logFile)), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	// Two tool calls in sequence
	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		toolSSE030("t2", "read", `{"file_path":"/dev/null"}`),
		textSSE030("Done."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})
	drainEvents(eng.Run(context.Background(), "scan"))

	// Check hook fired twice
	data, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Errorf("Hook should fire on each tool call, fired %d times", len(lines))
	}
}

// =============================================================================
// 8. EXEC MODE JSON OUTPUT PARSING
// =============================================================================

func TestE2E_ExecJSONContainsAllEventTypes(t *testing.T) {
	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		textSSE030("Final."),
	})
	defer srv.Close()
	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgForServer(srv)},
		Prompt:       "list",
		JSON:         true,
		Writer:       &buf,
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	types := make(map[event.EventType]bool)
	for _, line := range lines {
		var ev event.Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			types[ev.Type] = true
		}
	}

	// Should have at minimum: ToolStart, ToolResult, TextDelta, Done
	for _, required := range []event.EventType{event.ToolStart, event.ToolResultEvent, event.TextDelta, event.Done} {
		if !types[required] {
			t.Errorf("Missing event type %q in JSON output", required)
		}
	}
}

func TestE2E_ExecJSONEachLineValid(t *testing.T) {
	srv := multiCallServer(t, []string{textSSE030("hi")})
	defer srv.Close()
	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfgForServer(srv)},
		Prompt:       "hi",
		JSON:         true,
		Writer:       &buf,
	})

	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Errorf("Invalid JSON line: %q", line)
		}
	}
}

// =============================================================================
// 9. HOOK TIMEOUT AND ERROR HANDLING
// =============================================================================

func TestE2E_HookTimeoutDefaultsToAllow(t *testing.T) {
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks: []hooks.EntryConfig{{
				Type:    "command",
				Command: "sleep 10", // will timeout
				Timeout: 1,          // 1 second timeout
			}},
		}},
	})

	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		textSSE030("Done."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	start := time.Now()
	events := drainEvents(eng.Run(context.Background(), "list"))
	elapsed := time.Since(start)

	// Should complete in ~1-2s (timeout), not 10s
	if elapsed > 5*time.Second {
		t.Errorf("Hook timeout didn't work, took %v", elapsed)
	}
	if countEv(events, event.Done) != 1 {
		t.Error("Should still complete after hook timeout")
	}
}

func TestE2E_HookCommandNotFound(t *testing.T) {
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "nonexistent_program_xyz"}},
		}},
	})

	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		textSSE030("Done."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "list"))

	// Should still complete — hook errors default to allow
	if countEv(events, event.Done) != 1 {
		t.Error("Should complete despite hook error")
	}
}

func TestE2E_HookMalformedJSONOutput(t *testing.T) {
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "echo 'not json at all'"}},
		}},
	})

	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		textSSE030("Done."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "list"))

	// Malformed JSON should default to allow
	if countEv(events, event.Done) != 1 {
		t.Error("Malformed JSON hook output should default to allow")
	}
}

// =============================================================================
// 10. ADDITIONAL EDGE CASES
// =============================================================================

func TestE2E_ExecWithSessionPersistence(t *testing.T) {
	db, _ := store.Open(":memory:")
	defer db.Close()
	sess, _ := db.CreateSession("proj", "exec-persist", "test")

	srv := multiCallServer(t, []string{textSSE030("Saved!")})
	defer srv.Close()

	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{
			Config: cfgForServer(srv), Store: db, SessionID: sess.ID,
		},
		Prompt: "save this",
		Writer: &buf,
	})

	msgs, _ := db.ListMessages(sess.ID)
	if len(msgs) < 2 {
		t.Fatalf("Expected persisted messages, got %d", len(msgs))
	}
}

func TestE2E_CommandDiscoverEmpty(t *testing.T) {
	cmds, _ := command.Discover("/nonexistent/path")
	if len(cmds) != 0 {
		t.Error("Expected empty")
	}
}

func TestE2E_HookPipeSeparatedMatcher(t *testing.T) {
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "write|edit",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: `echo '{"decision":"deny","message":"readonly"}'`}},
		}},
	})

	srv := multiCallServer(t, []string{
		toolSSE030("t1", "write", `{"file_path":"/tmp/x","content":"y"}`),
		textSSE030("Blocked."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		Hooks:  hookRunner,
	})

	events := drainEvents(eng.Run(context.Background(), "write file"))

	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "readonly") {
				return // PASS
			}
		}
	}
	t.Error("Pipe-separated matcher should deny write tool")
}

func TestE2E_NoHooksStillWorks(t *testing.T) {
	srv := multiCallServer(t, []string{
		toolSSE030("t1", "ls", `{"path":"."}`),
		textSSE030("Done."),
	})
	defer srv.Close()

	eng, _ := engine.New(engine.EngineParams{
		Config: cfgForServer(srv),
		// No hooks
	})

	events := drainEvents(eng.Run(context.Background(), "list"))
	if countEv(events, event.Done) != 1 {
		t.Error("Should work without hooks")
	}
}

func TestE2E_PluginEmptyDir(t *testing.T) {
	dir := t.TempDir()
	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 0 {
		t.Error("Empty dir should have no plugins")
	}
}

func TestE2E_CommandExpandNoBackticks(t *testing.T) {
	cmd := &command.Command{Body: "Plain text with no templates."}
	result, err := cmd.Expand("")
	if err != nil {
		t.Fatal(err)
	}
	if result != "Plain text with no templates." {
		t.Errorf("Should pass through: %q", result)
	}
}

func TestE2E_StoreMessageContentFallback(t *testing.T) {
	// Legacy plain text (not JSON) in store
	stored := []*store.Message{
		{Role: "user", Content: []byte("plain text message")},
	}
	converted := store.ToProviderMessages(stored)
	if converted[0].Content != "plain text message" {
		t.Errorf("Fallback failed: %q", converted[0].Content)
	}
}
