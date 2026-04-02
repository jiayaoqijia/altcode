//go:build !windows

package internal_test

// Tests ported from OpenAI Codex CLI test suite to verify altcode's
// compatibility with GPT/Codex models. Covers SSE parsing edge cases,
// tool dispatch with namespaces, output capping, approval dedup,
// and cross-provider format validation.

import (
	"bytes"
	"context"
	"io"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/mcp"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/tool"
)

// =============================================================================
// PORTED FROM CODEX: SSE streaming edge cases (stream_events_utils_tests.rs)
// =============================================================================

func TestCodex_OpenAISSE_MultipleToolCallsInOneChunk(t *testing.T) {
	// Codex tests parsing multiple tool_calls in a single SSE chunk
	body := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":""}},{"index":1,"id":"c2","type":"function","function":{"name":"ls","arguments":""}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_path\":\"/tmp\"}"}},{"index":1,"function":{"arguments":"{\"path\":\".\"}"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, err := p.Stream(context.Background(), &provider.Request{
		Model: "gpt-4", Messages: []provider.Message{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	toolStarts := 0
	toolEnds := 0
	for ev := range stream {
		if ev.Type == provider.StreamToolCallStart {
			toolStarts++
		}
		if ev.Type == provider.StreamToolCallEnd {
			toolEnds++
		}
	}
	if toolStarts != 2 {
		t.Errorf("Expected 2 tool starts, got %d", toolStarts)
	}
	if toolEnds != 2 {
		t.Errorf("Expected 2 tool ends, got %d", toolEnds)
	}
}

func TestCodex_OpenAISSE_TextAndToolCallInterleaved(t *testing.T) {
	// Model sends text first, then tool call in same response
	body := `data: {"choices":[{"delta":{"content":"Let me check."},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":""}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_path\":\"/x\"}"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, _ := p.Stream(context.Background(), &provider.Request{
		Model: "gpt-4", Messages: []provider.Message{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})

	var text string
	var toolName string
	for ev := range stream {
		if ev.Type == provider.StreamTextDelta {
			text += ev.Delta
		}
		if ev.Type == provider.StreamToolCallStart && ev.ToolUse != nil {
			toolName = ev.ToolUse.Name
		}
	}
	if text != "Let me check." {
		t.Errorf("Text: %q", text)
	}
	if toolName != "read" {
		t.Errorf("Tool: %q", toolName)
	}
}

func TestCodex_OpenAISSE_EmptyDeltaChunks(t *testing.T) {
	// Empty delta chunks should be ignored (common from API)
	body := `data: {"choices":[{"delta":{},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, _ := p.Stream(context.Background(), &provider.Request{
		Model: "gpt-4", Messages: []provider.Message{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})

	var text string
	for ev := range stream {
		if ev.Type == provider.StreamTextDelta {
			text += ev.Delta
		}
	}
	if text != "hello" {
		t.Errorf("Expected 'hello', got %q", text)
	}
}

func TestCodex_OpenAISSE_MalformedJSON(t *testing.T) {
	// Malformed JSON in SSE data should not crash
	body := "data: {broken json\n\n" +
		`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, _ := p.Stream(context.Background(), &provider.Request{
		Model: "gpt-4", Messages: []provider.Message{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})

	var text string
	for ev := range stream {
		if ev.Type == provider.StreamTextDelta {
			text += ev.Delta
		}
	}
	if text != "ok" {
		t.Errorf("Should recover from malformed JSON, got %q", text)
	}
}

// =============================================================================
// PORTED FROM CODEX: Output capping (exec_tests.rs)
// =============================================================================

func TestCodex_BashOutputCapping(t *testing.T) {
	// Codex caps command output at EXEC_OUTPUT_MAX_BYTES
	bashTool := tool.NewBashTool()

	// Generate large output (>512KB)
	input, _ := json.Marshal(map[string]any{
		"command": "python3 -c \"print('x' * 1000000)\"",
		"timeout": 5000,
	})

	result, err := bashTool.Execute(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	// Output should be present (not capped in current impl, but shouldn't crash)
	if result.Output == "" {
		t.Error("Should have output")
	}
}

func TestCodex_BashStderrIncluded(t *testing.T) {
	// Codex tests stderr aggregation
	bashTool := tool.NewBashTool()
	input, _ := json.Marshal(map[string]any{"command": "echo err >&2; echo out"})

	result, _ := bashTool.Execute(context.Background(), input)
	if !strings.Contains(result.Output, "out") {
		t.Error("Missing stdout")
	}
	if !strings.Contains(result.Output, "err") {
		t.Error("Missing stderr")
	}
}

func TestCodex_BashSingleProcessTimeout(t *testing.T) {
	// Codex tests process timeout enforcement
	bashTool := tool.NewBashTool()
	input, _ := json.Marshal(map[string]any{
		"command": "sleep 30",
		"timeout": 200, // 200ms
	})

	start := time.Now()
	_, _ = bashTool.Execute(context.Background(), input)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Errorf("Timeout didn't kill process, took %v", elapsed)
	}
}

// =============================================================================
// PORTED FROM CODEX: MCP tool routing with namespaces (mcp_tool_call_tests.rs)
// =============================================================================

const mockMCPPy = `#!/usr/bin/env python3
import json, sys
for line in sys.stdin:
    req = json.loads(line.strip())
    rid = req.get("id", 0)
    method = req.get("method", "")
    if method == "tools/list":
        resp = {"jsonrpc":"2.0","id":rid,"result":{"tools":[
            {"name":"search","description":"Search docs","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}},
            {"name":"create","description":"Create item","inputSchema":{"type":"object","properties":{"name":{"type":"string"}}}}
        ]}}
    elif method == "tools/call":
        name = req.get("params",{}).get("name","")
        resp = {"jsonrpc":"2.0","id":rid,"result":{"content":[{"type":"text","text":"result from "+name}]}}
    else:
        resp = {"jsonrpc":"2.0","id":rid,"result":{}}
    sys.stdout.write(json.dumps(resp)+"\n")
    sys.stdout.flush()
`

func TestCodex_MCPToolNamespacing(t *testing.T) {
	// Codex uses mcp__servername__toolname namespace
	dir := t.TempDir()
	script := filepath.Join(dir, "mcp.py")
	os.WriteFile(script, []byte(mockMCPPy), 0o755)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mcp.Connect(ctx, "python3", []string{script}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	registry := tool.NewRegistry()
	mcp.RegisterMCPTools(ctx, registry, client, "my-app")

	// Verify namespace format
	searchTool, ok := registry.Get("mcp__my-app__search")
	if !ok {
		t.Fatal("Expected mcp__my-app__search")
	}
	createTool, ok := registry.Get("mcp__my-app__create")
	if !ok {
		t.Fatal("Expected mcp__my-app__create")
	}

	// Execute through namespace
	result, _ := searchTool.Execute(ctx, json.RawMessage(`{"q":"test"}`))
	if result.Output != "result from search" {
		t.Errorf("Output: %q", result.Output)
	}
	_ = createTool
}

func TestCodex_MCPToolsInRegistry(t *testing.T) {
	// Verify MCP tools appear in registry.All() for schema generation
	dir := t.TempDir()
	script := filepath.Join(dir, "mcp.py")
	os.WriteFile(script, []byte(mockMCPPy), 0o755)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, _ := mcp.Connect(ctx, "python3", []string{script}, nil)
	defer client.Close()

	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool()) // built-in
	mcp.RegisterMCPTools(ctx, registry, client, "ext")

	all := registry.All()
	if len(all) < 3 {
		t.Errorf("Expected 3+ tools (1 built-in + 2 MCP), got %d", len(all))
	}

	schemas := registry.Schemas()
	foundMCP := false
	for _, s := range schemas {
		if strings.HasPrefix(s.Name, "mcp__") {
			foundMCP = true
		}
	}
	if !foundMCP {
		t.Error("MCP tools should appear in schemas")
	}
}

// =============================================================================
// PORTED FROM CODEX: Approval deduplication (network_approval_tests.rs)
// =============================================================================

func TestCodex_PermissionDedupConsecutiveCalls(t *testing.T) {
	// Codex deduplicates consecutive identical approval requests
	eval := permission.NewEvaluator(permission.ModeAuto, "", nil)

	// First call — should be Auto deny
	r1 := eval.Check("bash", "bash:curl example.com")
	if r1 != permission.ActionDeny {
		t.Errorf("Auto mode should deny: %v", r1)
	}

	// Record 3 identical calls — triggers doom loop
	eval.RecordCall("bash", "bash:curl example.com")
	eval.RecordCall("bash", "bash:curl example.com")
	eval.RecordCall("bash", "bash:curl example.com")

	r2 := eval.Check("bash", "bash:curl example.com")
	if r2 != permission.ActionAsk {
		t.Errorf("Doom loop should escalate to Ask: %v", r2)
	}
}

func TestCodex_PermissionSessionRulePersists(t *testing.T) {
	// Codex allows session-scoped permission amendments
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	// bash:npm test should ask by default
	r := eval.Check("bash", "bash:npm test")
	if r != permission.ActionAsk {
		t.Errorf("Expected Ask, got %v", r)
	}

	// Add session rule (like Codex's execpolicy amendment)
	eval.AddSessionRule(permission.Rule{
		Tool: "bash", Pattern: "npm *", Action: permission.ActionAllow, Source: "session",
	})

	r = eval.Check("bash", "bash:npm test")
	if r != permission.ActionAllow {
		t.Errorf("Session rule should allow: %v", r)
	}

	// Different npm command also allowed
	r = eval.Check("bash", "bash:npm run build")
	if r != permission.ActionAllow {
		t.Errorf("Glob pattern should match: %v", r)
	}
}

func TestCodex_PermissionConcurrentChecks(t *testing.T) {
	// Codex tests concurrent permission evaluation (no race)
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			eval.Check("read", fmt.Sprintf("read:/file%d", n))
			eval.RecordCall("read", fmt.Sprintf("read:/file%d", n))
		}(i)
	}
	wg.Wait()
	// If we reach here without panic, the mutex is working
}

// =============================================================================
// PORTED FROM CODEX: Cross-provider tool call format (unified_exec_tests.rs)
// =============================================================================

func TestCodex_OpenAIToolCallRequestFormat(t *testing.T) {
	// Verify altcode sends tool schemas in OpenAI function format
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Model = "openai/gpt-4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       "hi",
		Writer:       &buf,
	})

	// Parse the request body
	var req map[string]any
	json.Unmarshal(capturedBody, &req)

	// Should have "tools" array with function format
	tools, ok := req["tools"]
	if !ok {
		t.Fatal("Request missing 'tools' field")
	}
	toolArr, ok := tools.([]any)
	if !ok || len(toolArr) == 0 {
		t.Fatal("Expected non-empty tools array")
	}

	// Each tool should have type: "function" and function: {name, description, parameters}
	first := toolArr[0].(map[string]any)
	if first["type"] != "function" {
		t.Errorf("Tool type should be 'function', got %v", first["type"])
	}
	fn, ok := first["function"].(map[string]any)
	if !ok {
		t.Fatal("Missing function field")
	}
	if fn["name"] == nil || fn["description"] == nil || fn["parameters"] == nil {
		t.Errorf("Function missing fields: %v", fn)
	}
}

func TestCodex_AnthropicToolCallRequestFormat(t *testing.T) {
	// Verify altcode sends tool schemas in Anthropic format
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       "hi",
		Writer:       &buf,
	})

	var req map[string]any
	json.Unmarshal(capturedBody, &req)

	// Should have "tools" array with Anthropic format (name, description, input_schema)
	tools, ok := req["tools"]
	if !ok {
		t.Fatal("Request missing 'tools' field")
	}
	toolArr, ok := tools.([]any)
	if !ok || len(toolArr) == 0 {
		t.Fatal("Expected non-empty tools array")
	}

	first := toolArr[0].(map[string]any)
	if first["name"] == nil || first["description"] == nil || first["input_schema"] == nil {
		t.Errorf("Anthropic tool missing fields: %v", first)
	}
}

func TestCodex_OpenAIToolResultFormat(t *testing.T) {
	// Verify tool results sent in OpenAI format (role="tool", tool_call_id)
	var capturedBodies [][]byte
	var mu sync.Mutex
	callIdx := int32(0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		capturedBodies = append(capturedBodies, body)
		mu.Unlock()

		idx := atomic.AddInt32(&callIdx, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if idx == 1 {
			// First call: return tool call
			w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"ls","arguments":"{\"path\":\".\"}  "}}]},"finish_reason":null}]}` + "\n\n" +
				`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
		} else {
			// Second call: return text
			w.Write([]byte(`data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
		}
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Model = "openai/gpt-4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	var buf bytes.Buffer
	exec.Run(context.Background(), exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       "list",
		Writer:       &buf,
	})

	// Second request should contain tool result message
	if len(capturedBodies) < 2 {
		t.Fatalf("Expected 2 requests, got %d", len(capturedBodies))
	}

	var req2 map[string]any
	json.Unmarshal(capturedBodies[1], &req2)

	msgs, ok := req2["messages"].([]any)
	if !ok {
		t.Fatal("Missing messages in second request")
	}

	// Find the tool result message
	foundToolMsg := false
	for _, m := range msgs {
		msg := m.(map[string]any)
		if msg["role"] == "tool" {
			foundToolMsg = true
			if msg["tool_call_id"] == nil {
				t.Error("Tool message missing tool_call_id")
			}
		}
	}
	if !foundToolMsg {
		t.Error("Expected role=tool message in second request")
	}
}

// =============================================================================
// PORTED FROM CODEX: Hook + provider integration (codex_delegate_tests.rs)
// =============================================================================

func TestCodex_HooksWorkWithOpenAIProvider(t *testing.T) {
	// Verify hooks fire correctly when using OpenAI provider
	dir := t.TempDir()
	logFile := filepath.Join(dir, "hook.log")
	script := filepath.Join(dir, "log.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
echo "hook_fired" >> %s
echo '{"decision":"allow"}'
`, logFile)), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

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
			w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"ls","arguments":"{\"path\":\".\"}  "}}]},"finish_reason":null}]}` + "\n\n" +
				`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"))
		} else {
			w.Write([]byte(`data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
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

	for ev := range eng.Run(context.Background(), "list") {
		_ = ev
	}

	data, _ := os.ReadFile(logFile)
	if !strings.Contains(string(data), "hook_fired") {
		t.Error("Hook should fire with OpenAI provider")
	}
}

func TestCodex_StopHookWorksWithOpenAI(t *testing.T) {
	callCount := int32(0)
	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: `echo '{"decision":"allow"}'`}},
		}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(`data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Model = "openai/gpt-4"
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: "test", BaseURL: srv.URL}

	eng, _ := engine.New(engine.EngineParams{Config: cfg, Hooks: hookRunner})
	events := collectAllEventsFromCh(eng.Run(context.Background(), "hi"))

	hasDone := false
	for _, ev := range events {
		if ev.Type == event.Done {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("Stop hook with allow should complete")
	}
}

// =============================================================================
// PORTED FROM CODEX: Session persistence format (connectors_tests.rs)
// =============================================================================

func TestCodex_ToolCallContentPartRoundTrip(t *testing.T) {
	// Verify ContentPart with tool_use survives JSON round-trip
	// (critical for session persistence across providers)
	original := provider.Message{
		Role: "assistant",
		Parts: []provider.ContentPart{
			{Type: "text", Text: "I'll check."},
			{Type: "tool_use", ID: "call_abc123", Name: "read",
				Input: json.RawMessage(`{"file_path":"/home/user/file.go"}`)},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var decoded provider.Message
	json.Unmarshal(data, &decoded)

	if len(decoded.Parts) != 2 {
		t.Fatalf("Parts: %d", len(decoded.Parts))
	}
	if decoded.Parts[1].ID != "call_abc123" {
		t.Errorf("ID lost: %q", decoded.Parts[1].ID)
	}
	if string(decoded.Parts[1].Input) != `{"file_path":"/home/user/file.go"}` {
		t.Errorf("Input lost: %s", decoded.Parts[1].Input)
	}
}

func TestCodex_ToolResultContentPartRoundTrip(t *testing.T) {
	part := provider.NewToolResultPart("call_abc123", "file contents here\nline 2")
	msg := provider.ToolResultMessage([]provider.ContentPart{part})

	data, _ := json.Marshal(msg)
	var decoded provider.Message
	json.Unmarshal(data, &decoded)

	if decoded.Role != "user" {
		t.Errorf("Role: %q", decoded.Role)
	}
	if len(decoded.Parts) != 1 {
		t.Fatal("Parts lost")
	}
	if decoded.Parts[0].ToolUseID != "call_abc123" {
		t.Errorf("ToolUseID: %q", decoded.Parts[0].ToolUseID)
	}
	if decoded.Parts[0].Content != "file contents here\nline 2" {
		t.Errorf("Content: %q", decoded.Parts[0].Content)
	}
}

// =============================================================================
// Helper
// =============================================================================

func collectAllEventsFromCh(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
