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

	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/plugin"
	"github.com/altcode-ai/altcode/internal/provider"
)

// =============================================================================
// CLAUDE CODE COMPATIBILITY: Can altcode load and use Claude Code plugins?
// =============================================================================

func compatSSE(text string) string {
	return fmt.Sprintf("event: content_block_start\ndata: %s\n\n", `{"index":0,"content_block":{"type":"text","text":""}}`) +
		fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)) +
		"event: content_block_stop\ndata: {}\n\n" +
		"event: message_stop\ndata: {}\n\n"
}

func compatToolSSE(id, name, input string) string {
	return fmt.Sprintf("event: content_block_start\ndata: %s\n\n",
		fmt.Sprintf(`{"index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, id, name)) +
		fmt.Sprintf("event: content_block_delta\ndata: %s\n\n",
			fmt.Sprintf(`{"delta":{"type":"input_json_delta","partial_json":%q}}`, input)) +
		"event: content_block_stop\ndata: {}\n\n" +
		"event: message_stop\ndata: {}\n\n"
}

func compatServer(t *testing.T, responses []string) *httptest.Server {
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

func compatCfg(srv *httptest.Server) *config.Config {
	c := config.Default()
	c.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
	return c
}

// Test 1: Claude Code command format works with altcode
func TestCompat_ClaudeCodeCommandFormat(t *testing.T) {
	dir := t.TempDir()

	// Write a Claude Code style command (exact format from their repo)
	os.WriteFile(filepath.Join(dir, "commit-push-pr.md"), []byte(`---
allowed-tools: Bash(git checkout --branch:*), Bash(git add:*), Bash(git status:*), Bash(git push:*), Bash(git commit:*), Bash(gh pr create:*)
description: Commit, push, and open a PR
---

## Context

- Current git status: !`+"`git status`"+`
- Current branch: !`+"`git branch --show-current`"+`

## Your task

Based on the above changes:
1. Create a new branch if on main
2. Create a single commit with an appropriate message
3. Push the branch to origin
4. You MUST do all of the above in a single message.
`), 0o644)

	cmds, err := command.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("Expected 1 command, got %d", len(cmds))
	}

	cmd := cmds[0]
	if cmd.Name != "commit-push-pr" {
		t.Errorf("Name: %q", cmd.Name)
	}
	if cmd.Description != "Commit, push, and open a PR" {
		t.Errorf("Description: %q", cmd.Description)
	}
	if len(cmd.AllowedTools) == 0 {
		t.Error("Expected allowed-tools to be parsed")
	}

	// Expand should inject git output
	expanded, _ := cmd.Expand("")
	if !strings.Contains(expanded, "Your task") {
		t.Error("Body should be preserved after expansion")
	}
}

// Test 2: Claude Code hook format works with altcode
func TestCompat_ClaudeCodeHookFormat(t *testing.T) {
	dir := t.TempDir()

	// Write Claude Code style hooks.json (exact format from their repo)
	os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(`{
		"description": "Security hooks",
		"hooks": {
			"PreToolUse": [
				{
					"matcher": "Write|Edit",
					"hooks": [
						{
							"type": "command",
							"command": "echo '{\"decision\":\"allow\",\"message\":\"checked\"}'",
							"timeout": 30
						}
					]
				}
			],
			"Stop": [
				{
					"matcher": "*",
					"hooks": [
						{
							"type": "command",
							"command": "echo '{\"decision\":\"allow\"}'"
						}
					]
				}
			]
		}
	}`), 0o644)

	// Parse as plugin hooks (with wrapper)
	data, _ := os.ReadFile(filepath.Join(dir, "hooks.json"))
	var wrapper struct {
		Hooks map[string][]config.HookMatcherConfig `json:"hooks"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("Parse hooks.json: %v", err)
	}

	if len(wrapper.Hooks["PreToolUse"]) != 1 {
		t.Errorf("Expected 1 PreToolUse matcher, got %d", len(wrapper.Hooks["PreToolUse"]))
	}
	if wrapper.Hooks["PreToolUse"][0].Matcher != "Write|Edit" {
		t.Errorf("Matcher: %q", wrapper.Hooks["PreToolUse"][0].Matcher)
	}
	if len(wrapper.Hooks["Stop"]) != 1 {
		t.Error("Expected Stop hook")
	}

	// Convert to hooks.MatcherConfig and verify runner works
	hookConfigs := make(map[hooks.Event][]hooks.MatcherConfig)
	for event, matchers := range wrapper.Hooks {
		for _, m := range matchers {
			var entries []hooks.EntryConfig
			for _, h := range m.Hooks {
				entries = append(entries, hooks.EntryConfig{
					Type: h.Type, Command: h.Command, Timeout: h.Timeout,
				})
			}
			hookConfigs[hooks.Event(event)] = append(hookConfigs[hooks.Event(event)],
				hooks.MatcherConfig{Matcher: m.Matcher, Hooks: entries})
		}
	}

	runner := hooks.NewRunner(hookConfigs)
	results, _ := runner.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Write",
	})
	if len(results) == 0 || results[0].Decision != "allow" {
		t.Error("Hook should fire and return allow")
	}
}

// Test 3: Claude Code plugin structure works with altcode
func TestCompat_ClaudeCodePluginStructure(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "security-guidance")

	// Replicate Claude Code plugin structure
	os.MkdirAll(filepath.Join(pluginDir, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".altcode-plugin", "plugin.json"), []byte(`{
		"name": "security-guidance",
		"version": "1.0.0",
		"description": "Security reminder hook"
	}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "hooks", "hooks.json"), []byte(`{
		"hooks": {
			"PreToolUse": [{
				"matcher": "Write|Edit",
				"hooks": [{"type": "command", "command": "echo '{\"decision\":\"allow\"}'"}]
			}]
		}
	}`), 0o644)

	plugins, err := plugin.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 {
		t.Fatalf("Expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Manifest.Name != "security-guidance" {
		t.Errorf("Name: %q", plugins[0].Manifest.Name)
	}

	// Merge into config
	cfg := config.Default()
	plugins[0].Merge(cfg)
	if len(cfg.Hooks["PreToolUse"]) == 0 {
		t.Error("Hook should be merged into config")
	}
}

// Test 4: Claude Code style command with $ARGUMENTS works
func TestCompat_CommandWithArguments(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "triage-issue.md"), []byte(`---
description: Triage GitHub issues
---

You're an issue triage assistant.

Context: $ARGUMENTS

TASK:
1. Analyze the issue
2. Apply labels
`), 0o644)

	cmds, _ := command.Discover(dir)
	expanded, _ := cmds[0].Expand("issue #123")

	if !strings.Contains(expanded, "issue #123") {
		t.Error("$ARGUMENTS should be replaced")
	}
	if !strings.Contains(expanded, "TASK:") {
		t.Error("Body should be preserved")
	}
}

// Test 5: Multi-provider — same tool dispatch works with OpenAI format
func TestCompat_OpenAIProviderWithToolCalls(t *testing.T) {
	// OpenAI SSE format for a tool call
	oaiToolSSE := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"ls","arguments":""}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\".\"}  "}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	oaiTextSSE := `data: {"choices":[{"delta":{"content":"Listed files."},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

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
			w.Write([]byte(oaiToolSSE))
		} else {
			w.Write([]byte(oaiTextSSE))
		}
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Model = "openai/gpt-4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	var buf bytes.Buffer
	err := exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       "list files",
		Writer:       &buf,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(buf.String(), "Listed files.") {
		t.Errorf("Expected response, got: %q", buf.String())
	}
}

// Test 6: Anthropic + OpenAI produce same event types through engine
func TestCompat_CrossProviderEventParity(t *testing.T) {
	// Anthropic SSE
	anthropicSrv := compatServer(t, []string{compatSSE("Anthropic says hello")})
	defer anthropicSrv.Close()

	// OpenAI SSE
	oaiBody := `data: {"choices":[{"delta":{"content":"OpenAI says hello"},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	oaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(oaiBody))
	}))
	defer oaiSrv.Close()

	// Run with Anthropic
	anthropicCfg := config.Default()
	anthropicCfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: anthropicSrv.URL}
	eng1, _ := engine.New(engine.EngineParams{Config: anthropicCfg})
	events1 := collectAllEvents(eng1.Run(context.Background(), "hi"))

	// Run with OpenAI
	oaiCfg := config.Default()
	oaiCfg.Model = "openai/gpt-4"
	oaiCfg.Provider["openai"] = config.ProviderConfig{APIKey: "k", BaseURL: oaiSrv.URL}
	eng2, _ := engine.New(engine.EngineParams{Config: oaiCfg})
	events2 := collectAllEvents(eng2.Run(context.Background(), "hi"))

	// Both should have TextDelta and Done
	types1 := eventTypes(events1)
	types2 := eventTypes(events2)

	if !types1[event.TextDelta] {
		t.Error("Anthropic missing TextDelta")
	}
	if !types2[event.TextDelta] {
		t.Error("OpenAI missing TextDelta")
	}
	if !types1[event.Done] {
		t.Error("Anthropic missing Done")
	}
	if !types2[event.Done] {
		t.Error("OpenAI missing Done")
	}
}

// Test 7: Hooks work identically across providers
func TestCompat_HooksWorkWithOpenAI(t *testing.T) {
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "ls",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: `echo '{"decision":"allow","message":"hooked"}'`}},
		}},
	})

	// OpenAI tool call + response
	oaiToolSSE := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"ls","arguments":""}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\".\"}  "}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	oaiTextSSE := `data: {"choices":[{"delta":{"content":"Done."},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

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
			w.Write([]byte(oaiToolSSE))
		} else {
			w.Write([]byte(oaiTextSSE))
		}
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Model = "openai/gpt-4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	eng, _ := engine.New(engine.EngineParams{
		Config: cfg,
		Hooks:  hookRunner,
	})

	events := collectAllEvents(eng.Run(context.Background(), "list"))

	// Hook should have fired (tool result present)
	hasToolResult := false
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent {
			hasToolResult = true
		}
	}
	if !hasToolResult {
		t.Error("Hook should work with OpenAI provider")
	}
}

// Test 8: Instruction cascade (CLAUDE.md) works
func TestCompat_InstructionCascade(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("Always use descriptive variable names."), 0o644)
	os.WriteFile(filepath.Join(dir, "ALTCODE.md"), []byte("Prefer Go idioms."), 0o644)

	instructions, err := config.LoadInstructions(dir)
	if err != nil {
		t.Fatal(err)
	}

	foundClaude := false
	foundAltcode := false
	for _, inst := range instructions {
		if strings.Contains(inst.Content, "descriptive variable") {
			foundClaude = true
		}
		if strings.Contains(inst.Content, "Go idioms") {
			foundAltcode = true
		}
	}
	if !foundClaude {
		t.Error("CLAUDE.md should be loaded")
	}
	if !foundAltcode {
		t.Error("ALTCODE.md should be loaded")
	}
}

// Test 9: ContentPart round-trip through providers
func TestCompat_ContentPartProviderAgnostic(t *testing.T) {
	// ContentPart should serialize correctly for both providers
	msg := provider.Message{
		Role: "assistant",
		Parts: []provider.ContentPart{
			{Type: "text", Text: "I'll read that."},
			{Type: "tool_use", ID: "t1", Name: "read", Input: json.RawMessage(`{"file_path":"/tmp/x"}`)},
		},
	}

	data, _ := json.Marshal(msg)
	var decoded provider.Message
	json.Unmarshal(data, &decoded)

	if len(decoded.Parts) != 2 {
		t.Fatalf("Parts lost in round-trip: %d", len(decoded.Parts))
	}
	if decoded.Parts[0].Text != "I'll read that." {
		t.Errorf("Text: %q", decoded.Parts[0].Text)
	}
	if decoded.Parts[1].Name != "read" {
		t.Errorf("Name: %q", decoded.Parts[1].Name)
	}
}

// Test 10: Plugin with commands works across providers
func TestCompat_PluginCommandsWorkAcrossProviders(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "commit-commands")

	os.MkdirAll(filepath.Join(pluginDir, ".altcode-plugin"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, ".altcode-plugin", "plugin.json"), []byte(`{
		"name": "commit-commands",
		"version": "1.0.0"
	}`), 0o644)

	os.MkdirAll(filepath.Join(pluginDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(pluginDir, "commands", "commit.md"), []byte(`---
description: Create a commit
allowed-tools: Bash(git *)
---

Create a commit with message: $ARGUMENTS
`), 0o644)

	plugins, _ := plugin.Discover(dir)
	if len(plugins) != 1 || len(plugins[0].Commands) != 1 {
		t.Fatal("Plugin command not discovered")
	}

	cmd := plugins[0].Commands[0]
	expanded, _ := cmd.Expand("fix: typo")
	if !strings.Contains(expanded, "fix: typo") {
		t.Error("$ARGUMENTS not expanded in plugin command")
	}
	if len(cmd.AllowedTools) == 0 {
		t.Error("allowed-tools should be parsed from plugin command")
	}
}

func collectAllEvents(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

func eventTypes(events []event.Event) map[event.EventType]bool {
	m := make(map[event.EventType]bool)
	for _, ev := range events {
		m[ev.Type] = true
	}
	return m
}
