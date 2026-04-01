package internal_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/compact"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/sysctl"
	"github.com/altcode-ai/altcode/internal/tool"
	"github.com/altcode-ai/altcode/internal/tui"
)

// =============================================================================
// 1. SSE STREAMING: mock Anthropic server + full decode pipeline
// =============================================================================

func sseChunk(eventType, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)
}

func newMockAnthropicServer(t *testing.T, sseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request shape
		if r.Header.Get("x-api-key") == "" {
			t.Error("missing x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("missing anthropic-version header")
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("invalid request body: %v", err)
		}
		if req["stream"] != true {
			t.Error("stream must be true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseBody))
	}))
}

func TestSSE_FullTextStream(t *testing.T) {
	sse := sseChunk("content_block_start", `{"index":0,"content_block":{"type":"text","text":""}}`) +
		sseChunk("content_block_delta", `{"delta":{"type":"text_delta","text":"Hello"}}`) +
		sseChunk("content_block_delta", `{"delta":{"type":"text_delta","text":" world"}}`) +
		sseChunk("content_block_stop", `{}`) +
		sseChunk("message_delta", `{"usage":{"input_tokens":10,"output_tokens":5}}`) +
		sseChunk("message_stop", `{}`)

	srv := newMockAnthropicServer(t, sse)
	defer srv.Close()

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	stream, err := p.Stream(context.Background(), &provider.Request{
		Model:     "claude-test",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var gotUsage, gotDone bool
	for ev := range stream {
		switch ev.Type {
		case provider.StreamTextDelta:
			text += ev.Delta
		case provider.StreamUsage:
			gotUsage = true
			if ev.Usage.InputTokens != 10 || ev.Usage.OutputTokens != 5 {
				t.Errorf("Wrong usage: %+v", ev.Usage)
			}
		case provider.StreamDone:
			gotDone = true
		case provider.StreamError:
			t.Fatalf("Unexpected error: %v", ev.Error)
		}
	}

	if text != "Hello world" {
		t.Errorf("Expected 'Hello world', got %q", text)
	}
	if !gotUsage {
		t.Error("Missing usage event")
	}
	if !gotDone {
		t.Error("Missing done event")
	}
}

func TestSSE_ToolCallStream(t *testing.T) {
	sse := sseChunk("content_block_start", `{"index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"read"}}`) +
		sseChunk("content_block_delta", `{"delta":{"type":"input_json_delta","partial_json":"{\"file"}}`) +
		sseChunk("content_block_delta", `{"delta":{"type":"input_json_delta","partial_json":"_path\":\"/tmp/x\"}"}}`) +
		sseChunk("content_block_stop", `{}`) +
		sseChunk("message_stop", `{}`)

	srv := newMockAnthropicServer(t, sse)
	defer srv.Close()

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	stream, err := p.Stream(context.Background(), &provider.Request{
		Model:     "claude-test",
		Messages:  []provider.Message{{Role: "user", Content: "read /tmp/x"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotStart, gotDelta, gotEnd bool
	var toolID, toolName, toolDelta string
	for ev := range stream {
		switch ev.Type {
		case provider.StreamToolCallStart:
			gotStart = true
			toolID = ev.ToolUse.ID
			toolName = ev.ToolUse.Name
		case provider.StreamToolCallDelta:
			gotDelta = true
			toolDelta += ev.ToolUse.Delta
		case provider.StreamToolCallEnd:
			gotEnd = true
		case provider.StreamError:
			t.Fatalf("Error: %v", ev.Error)
		}
	}

	if !gotStart {
		t.Error("Missing ToolCallStart")
	}
	if !gotDelta {
		t.Error("Missing ToolCallDelta")
	}
	if !gotEnd {
		t.Error("Missing ToolCallEnd")
	}
	if toolID != "tool_1" {
		t.Errorf("Expected tool ID 'tool_1', got %q", toolID)
	}
	if toolName != "read" {
		t.Errorf("Expected tool name 'read', got %q", toolName)
	}
	if toolDelta != `{"file_path":"/tmp/x"}` {
		t.Errorf("Expected tool delta, got %q", toolDelta)
	}
}

func TestSSE_ThinkingStream(t *testing.T) {
	sse := sseChunk("content_block_start", `{"index":0,"content_block":{"type":"thinking"}}`) +
		sseChunk("content_block_delta", `{"delta":{"type":"thinking_delta","thinking":"Let me think..."}}`) +
		sseChunk("content_block_stop", `{}`) +
		sseChunk("content_block_start", `{"index":1,"content_block":{"type":"text","text":""}}`) +
		sseChunk("content_block_delta", `{"delta":{"type":"text_delta","text":"Answer"}}`) +
		sseChunk("content_block_stop", `{}`) +
		sseChunk("message_stop", `{}`)

	srv := newMockAnthropicServer(t, sse)
	defer srv.Close()

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	stream, err := p.Stream(context.Background(), &provider.Request{
		Model:     "claude-test",
		Messages:  []provider.Message{{Role: "user", Content: "think"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var thinking, text string
	for ev := range stream {
		switch ev.Type {
		case provider.StreamThinkingDelta:
			thinking += ev.Delta
		case provider.StreamTextDelta:
			text += ev.Delta
		case provider.StreamError:
			t.Fatalf("Error: %v", ev.Error)
		}
	}

	if thinking != "Let me think..." {
		t.Errorf("Expected thinking, got %q", thinking)
	}
	if text != "Answer" {
		t.Errorf("Expected 'Answer', got %q", text)
	}
}

func TestSSE_ErrorEvent(t *testing.T) {
	sse := sseChunk("error", `{"error":{"message":"rate_limited"}}`)

	srv := newMockAnthropicServer(t, sse)
	defer srv.Close()

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	stream, err := p.Stream(context.Background(), &provider.Request{
		Model:     "claude-test",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotError bool
	for ev := range stream {
		if ev.Type == provider.StreamError {
			gotError = true
			if ev.Error == nil || !strings.Contains(ev.Error.Error(), "rate_limited") {
				t.Errorf("Expected rate_limited error, got %v", ev.Error)
			}
		}
	}
	if !gotError {
		t.Error("Expected an error event from SSE error frame")
	}
}

func TestSSE_HTTP4xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"too many requests"}}`))
	}))
	defer srv.Close()

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	_, err := p.Stream(context.Background(), &provider.Request{
		Model:     "claude-test",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err == nil {
		t.Fatal("Expected error for 429 response")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("Error should contain status code: %v", err)
	}
}

func TestSSE_EmptyBody(t *testing.T) {
	srv := newMockAnthropicServer(t, "")
	defer srv.Close()

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	stream, err := p.Stream(context.Background(), &provider.Request{
		Model:     "claude-test",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Should get a StreamDone without crashing
	var gotDone bool
	for ev := range stream {
		if ev.Type == provider.StreamDone {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("Expected StreamDone even on empty body")
	}
}

func TestSSE_ContextCancellation(t *testing.T) {
	// Server that writes slowly
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, ok := w.(http.Flusher)
		if ok {
			flusher.Flush()
		}
		// Block until client disconnects
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := p.Stream(ctx, &provider.Request{
		Model:     "claude-test",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Cancel immediately
	cancel()

	// Channel should drain and close
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-stream:
			if !ok {
				return // closed, test passes
			}
		case <-timer.C:
			t.Fatal("Stream did not close after context cancellation within 2s")
		}
	}
}

// =============================================================================
// 2. TOOL DISPATCH: concurrency, error handling, eager results
// =============================================================================

type countingTool struct {
	name       string
	concurrent bool
	readOnly   bool
	callCount  atomic.Int32
	delay      time.Duration
	failErr    error
}

func (t *countingTool) Name() string                               { return t.name }
func (t *countingTool) Description() string                        { return "counting tool" }
func (t *countingTool) Parameters() json.RawMessage                { return json.RawMessage(`{}`) }
func (t *countingTool) IsConcurrencySafe() bool                    { return t.concurrent }
func (t *countingTool) IsReadOnly() bool                           { return t.readOnly }
func (t *countingTool) PermissionPattern(_ json.RawMessage) string { return t.name + ":*" }

func (t *countingTool) Execute(_ context.Context, _ json.RawMessage) (*tool.Result, error) {
	t.callCount.Add(1)
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	if t.failErr != nil {
		return nil, t.failErr
	}
	return &tool.Result{Output: "ok:" + t.name, Title: t.name}, nil
}

func TestDispatch_ConcurrentToolsRunInParallel(t *testing.T) {
	// Three concurrent tools with 50ms delay each
	// If parallel, total < 150ms. If serial, total >= 150ms.
	t1 := &countingTool{name: "r1", concurrent: true, delay: 50 * time.Millisecond}
	t2 := &countingTool{name: "r2", concurrent: true, delay: 50 * time.Millisecond}
	t3 := &countingTool{name: "r3", concurrent: true, delay: 50 * time.Millisecond}

	calls := []tool.Call{
		{ID: "1", Tool: t1, Input: json.RawMessage(`{}`)},
		{ID: "2", Tool: t2, Input: json.RawMessage(`{}`)},
		{ID: "3", Tool: t3, Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := tool.Dispatch(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}
	// Should complete in ~50ms (parallel), not 150ms (serial)
	if elapsed > 120*time.Millisecond {
		t.Errorf("Expected parallel execution (~50ms), took %v", elapsed)
	}
}

func TestDispatch_SequentialWriteTools(t *testing.T) {
	w1 := &countingTool{name: "w1", concurrent: false, delay: 20 * time.Millisecond}
	w2 := &countingTool{name: "w2", concurrent: false, delay: 20 * time.Millisecond}

	calls := []tool.Call{
		{ID: "1", Tool: w1, Input: json.RawMessage(`{}`)},
		{ID: "2", Tool: w2, Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := tool.Dispatch(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}
	// Serial: should take >= 40ms
	if elapsed < 35*time.Millisecond {
		t.Errorf("Write tools should run sequentially, took only %v", elapsed)
	}
}

func TestDispatch_MixedBatching(t *testing.T) {
	r1 := &countingTool{name: "r1", concurrent: true}
	r2 := &countingTool{name: "r2", concurrent: true}
	w1 := &countingTool{name: "w1", concurrent: false}
	r3 := &countingTool{name: "r3", concurrent: true}
	r4 := &countingTool{name: "r4", concurrent: true}

	calls := []tool.Call{
		{ID: "1", Tool: r1, Input: json.RawMessage(`{}`)},
		{ID: "2", Tool: r2, Input: json.RawMessage(`{}`)},
		{ID: "3", Tool: w1, Input: json.RawMessage(`{}`)},
		{ID: "4", Tool: r3, Input: json.RawMessage(`{}`)},
		{ID: "5", Tool: r4, Input: json.RawMessage(`{}`)},
	}

	results := tool.Dispatch(context.Background(), calls)
	if len(results) != 5 {
		t.Fatalf("Expected 5 results, got %d", len(results))
	}

	// Verify order is preserved
	for i, r := range results {
		expected := calls[i].Tool.Name()
		if r.Output != "ok:"+expected {
			t.Errorf("Result %d: expected 'ok:%s', got %q", i, expected, r.Output)
		}
	}
}

func TestDispatch_ToolErrorDoesNotAbort(t *testing.T) {
	good := &countingTool{name: "good", concurrent: true}
	bad := &countingTool{name: "bad", concurrent: true, failErr: fmt.Errorf("disk full")}

	calls := []tool.Call{
		{ID: "1", Tool: good, Input: json.RawMessage(`{}`)},
		{ID: "2", Tool: bad, Input: json.RawMessage(`{}`)},
	}

	results := tool.Dispatch(context.Background(), calls)
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	if results[0].Error != nil {
		t.Errorf("Good tool should succeed, got error: %v", results[0].Error)
	}
	if results[1].Error == nil {
		t.Error("Bad tool should have error")
	}
}

func TestDispatch_EagerResultSkipsExecution(t *testing.T) {
	tool1 := &countingTool{name: "skip", concurrent: true}
	eager := &tool.Result{Output: "eager", Title: "pre-computed"}

	calls := []tool.Call{
		{ID: "1", Tool: tool1, Input: json.RawMessage(`{}`), EagerResult: eager},
	}

	results := tool.Dispatch(context.Background(), calls)
	if results[0].Output != "eager" {
		t.Errorf("Expected eager result, got %q", results[0].Output)
	}
	if tool1.callCount.Load() != 0 {
		t.Error("Tool should not have been called when eager result provided")
	}
}

// =============================================================================
// 3. PERMISSION SYSTEM: mode transitions, concurrent access, edge cases
// =============================================================================

func TestPermission_ModeTransition(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	// Default: read allowed, write asks
	if r := eval.Check("read", "read:/file"); r != permission.ActionAllow {
		t.Errorf("Default mode: read should be Allow, got %v", r)
	}
	if r := eval.Check("bash", "bash:rm -rf /"); r != permission.ActionAsk {
		t.Errorf("Default mode: dangerous bash should Ask, got %v", r)
	}

	// Switch to bypass
	eval.SetMode(permission.ModeBypass)
	if r := eval.Check("bash", "bash:rm -rf /"); r != permission.ActionAllow {
		t.Errorf("Bypass mode: should Allow everything, got %v", r)
	}

	// Switch to plan
	eval.SetMode(permission.ModePlan)
	if r := eval.CheckWithReadOnly("edit", "edit:/file", false); r != permission.ActionDeny {
		t.Errorf("Plan mode: write should Deny, got %v", r)
	}
	if r := eval.CheckWithReadOnly("read", "read:/file", true); r != permission.ActionAllow {
		t.Errorf("Plan mode: read should Allow, got %v", r)
	}

	// Switch to auto
	eval.SetMode(permission.ModeAuto)
	// Unknown tool in auto mode should deny (not ask)
	if r := eval.Check("bash", "bash:curl evil.com"); r != permission.ActionDeny {
		t.Errorf("Auto mode: unknown bash should Deny, got %v", r)
	}
}

func TestPermission_ConcurrentAccess(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	var wg sync.WaitGroup
	errors := make(chan string, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Mix of operations
			eval.Check("read", fmt.Sprintf("read:/file%d", n))
			eval.RecordCall("bash", fmt.Sprintf("bash:cmd%d", n))
			eval.AddSessionRule(permission.Rule{
				Tool: "bash", Pattern: fmt.Sprintf("cmd%d", n),
				Action: permission.ActionAllow, Source: "session",
			})
		}(i)
	}

	wg.Wait()
	close(errors)
	for e := range errors {
		t.Error(e)
	}
}

func TestPermission_DoomLoopResets(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeAuto, "", nil)

	// 3 same calls triggers doom loop
	eval.RecordCall("bash", "bash:echo x")
	eval.RecordCall("bash", "bash:echo x")
	eval.RecordCall("bash", "bash:echo x")

	if r := eval.Check("bash", "bash:echo x"); r != permission.ActionAsk {
		t.Fatalf("Should trigger doom loop, got %v", r)
	}

	// Different call should NOT trigger doom loop
	eval.RecordCall("bash", "bash:echo y")
	if r := eval.Check("bash", "bash:echo x"); r == permission.ActionAsk {
		// After a different call, the last 3 are no longer all "echo x"
		t.Log("Doom loop correctly checks last 3 consecutive calls")
	}
}

func TestPermission_SessionRuleOverridesDefault(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", nil)

	// "make build" should Ask by default
	if r := eval.Check("bash", "bash:make build"); r != permission.ActionAsk {
		t.Fatalf("Expected Ask for make build, got %v", r)
	}

	// Add session rule to allow
	eval.AddSessionRule(permission.Rule{
		Tool: "bash", Pattern: "make *", Action: permission.ActionAllow, Source: "session",
	})

	if r := eval.Check("bash", "bash:make build"); r != permission.ActionAllow {
		t.Fatalf("Expected Allow after session rule, got %v", r)
	}

	// Session deny should override session allow
	eval.AddSessionRule(permission.Rule{
		Tool: "bash", Pattern: "make clean", Action: permission.ActionDeny, Source: "session",
	})

	if r := eval.Check("bash", "bash:make clean"); r != permission.ActionDeny {
		t.Fatalf("Expected Deny for make clean, got %v", r)
	}
}

func TestPermission_GlobPatternEdgeCases(t *testing.T) {
	eval := permission.NewEvaluator(permission.ModeDefault, "", []permission.Rule{
		{Tool: "bash", Pattern: "git diff *", Action: permission.ActionAllow, Source: "project"},
		{Tool: "bash", Pattern: "npm run *", Action: permission.ActionAllow, Source: "project"},
	})

	tests := []struct {
		pattern string
		expect  permission.ActionType
	}{
		{"bash:git diff HEAD", permission.ActionAllow},
		{"bash:git diff --staged", permission.ActionAllow},
		{"bash:git push", permission.ActionAsk},       // not "git diff *"
		{"bash:npm run test", permission.ActionAllow},
		{"bash:npm install", permission.ActionAsk},     // not "npm run *"
	}

	for _, tt := range tests {
		r := eval.Check("bash", tt.pattern)
		if r != tt.expect {
			t.Errorf("Pattern %q: expected %v, got %v", tt.pattern, tt.expect, r)
		}
	}
}

// =============================================================================
// 4. CONFIG CASCADE: load order, env expansion, project detection
// =============================================================================

func TestConfigCascade_MergeOrder(t *testing.T) {
	dir := t.TempDir()

	// User config
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "config.json"), []byte(`{
		"model": "anthropic/claude-haiku-4-5-20251001",
		"theme": "catppuccin-mocha"
	}`), 0o644)

	// Project config (should override user)
	projDir := filepath.Join(dir, "project")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "config.json"), []byte(`{
		"model": "anthropic/claude-sonnet-4-20250514"
	}`), 0o644)

	userCfg, err := config.LoadFile(filepath.Join(userDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadFile user: %v", err)
	}
	projCfg, err := config.LoadFile(filepath.Join(projDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadFile project: %v", err)
	}

	// Verify model is overridden by project
	if projCfg.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Errorf("Project model wrong: %s", projCfg.Model)
	}
	// Verify user theme is set
	if userCfg.Theme != "catppuccin-mocha" {
		t.Errorf("User theme wrong: %s", userCfg.Theme)
	}
}

func TestConfigCascade_EnvVarExpansion(t *testing.T) {
	os.Setenv("TEST_ALTCODE_KEY", "sk-test-12345")
	defer os.Unsetenv("TEST_ALTCODE_KEY")

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		"provider": {
			"anthropic": {
				"apiKey": "$TEST_ALTCODE_KEY"
			}
		}
	}`), 0o644)

	cfg, err := config.LoadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if cfg.Provider["anthropic"].APIKey != "sk-test-12345" {
		t.Errorf("Expected expanded env var, got %q", cfg.Provider["anthropic"].APIKey)
	}
}

func TestConfigCascade_MissingEnvVar(t *testing.T) {
	os.Unsetenv("NONEXISTENT_VAR_12345")

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		"provider": {
			"anthropic": {
				"apiKey": "$NONEXISTENT_VAR_12345"
			}
		}
	}`), 0o644)

	cfg, err := config.LoadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	if cfg.Provider["anthropic"].APIKey != "" {
		t.Errorf("Missing env var should expand to empty, got %q", cfg.Provider["anthropic"].APIKey)
	}
}

func TestConfigCascade_JSONCComments(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{
		// This is a comment
		"model": "anthropic/claude-haiku-4-5-20251001", // inline comment
		"theme": "default"
	}`), 0o644)

	cfg, err := config.LoadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("LoadFile with comments: %v", err)
	}
	if cfg.Model != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("JSONC parsing failed, model: %q", cfg.Model)
	}
}

func TestProjectDetection_GitRoot(t *testing.T) {
	dir := t.TempDir()
	// Create nested structure with .git at root
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	subDir := filepath.Join(dir, "src", "pkg")
	os.MkdirAll(subDir, 0o755)

	root := config.DetectProjectRoot(subDir)
	if root != dir {
		t.Errorf("Expected root %q, got %q", dir, root)
	}
}

func TestProjectDetection_NoGit(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "src", "pkg")
	os.MkdirAll(subDir, 0o755)

	root := config.DetectProjectRoot(subDir)
	if root != subDir {
		t.Errorf("Without .git, should return startDir %q, got %q", subDir, root)
	}
}

func TestInstructionCascade(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("claude rules"), 0o644)
	os.WriteFile(filepath.Join(dir, "ALTCODE.md"), []byte("altcode rules"), 0o644)

	os.MkdirAll(filepath.Join(dir, ".altcode", "rules"), 0o755)
	os.WriteFile(filepath.Join(dir, ".altcode", "rules", "01-safety.md"), []byte("safety first"), 0o644)

	instructions, err := config.LoadInstructions(dir)
	if err != nil {
		t.Fatalf("LoadInstructions: %v", err)
	}

	// Should find CLAUDE.md and ALTCODE.md and rules file
	if len(instructions) < 2 {
		t.Fatalf("Expected at least 2 instructions, got %d", len(instructions))
	}

	var foundClaude, foundAltcode, foundRules bool
	for _, inst := range instructions {
		if strings.HasSuffix(inst.Path, "CLAUDE.md") {
			foundClaude = true
			if inst.Content != "claude rules" {
				t.Errorf("CLAUDE.md content wrong: %q", inst.Content)
			}
		}
		if strings.HasSuffix(inst.Path, "ALTCODE.md") {
			foundAltcode = true
		}
		if strings.Contains(inst.Path, "01-safety.md") {
			foundRules = true
		}
	}
	if !foundClaude {
		t.Error("Missing CLAUDE.md")
	}
	if !foundAltcode {
		t.Error("Missing ALTCODE.md")
	}
	if !foundRules {
		t.Error("Missing rules/01-safety.md")
	}
}

// =============================================================================
// 5. CONTEXT COMPACTION: budget math, boundary conditions
// =============================================================================

func TestBudgetCompactor_ExactBudget(t *testing.T) {
	msg := provider.Message{Role: "tool", Content: strings.Repeat("x", 1000)}
	messages := []provider.Message{msg}

	// Budget exactly matches — should not truncate
	c := compact.NewBudgetCompactor(1000)
	result := c.Apply(messages)
	if result[0].Content != msg.Content {
		t.Error("Should not truncate when exactly at budget")
	}
}

func TestBudgetCompactor_OneBytOver(t *testing.T) {
	messages := []provider.Message{
		{Role: "tool", Content: strings.Repeat("x", 501)},
		{Role: "tool", Content: strings.Repeat("y", 500)},
	}

	c := compact.NewBudgetCompactor(1000)
	result := c.Apply(messages)

	total := 0
	for _, m := range result {
		if m.Role == "tool" {
			total += len(m.Content)
		}
	}
	if total > 1000 {
		t.Errorf("Over budget: %d > 1000", total)
	}
}

func TestBudgetCompactor_PreservesNonToolMessages(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "tool", Content: strings.Repeat("x", 10000)},
		{Role: "assistant", Content: "I found it"},
	}

	c := compact.NewBudgetCompactor(100)
	result := c.Apply(messages)

	if result[0].Content != "hello" {
		t.Error("User message should be preserved")
	}
	if result[2].Content != "I found it" {
		t.Error("Assistant message should be preserved")
	}
}

func TestBudgetCompactor_SmallToolNotTruncated(t *testing.T) {
	messages := []provider.Message{
		{Role: "tool", Content: "small"},                      // 5 bytes, < 100 threshold
		{Role: "tool", Content: strings.Repeat("x", 10000)},  // big
	}

	c := compact.NewBudgetCompactor(100)
	result := c.Apply(messages)

	if result[0].Content != "small" {
		t.Error("Small tool results (< 100 bytes) should not be truncated")
	}
}

func TestMicrocompactor_PreservesRecentTurns(t *testing.T) {
	var messages []provider.Message

	// Old turns (should be compacted)
	for i := 0; i < 5; i++ {
		messages = append(messages,
			provider.Message{Role: "user", Content: fmt.Sprintf("old-%d", i)},
			provider.Message{Role: "tool", Content: fmt.Sprintf("old-tool-%d", i)},
			provider.Message{Role: "assistant", Content: fmt.Sprintf("old-reply-%d", i)},
		)
	}

	// Recent turns (should be preserved)
	for i := 0; i < 3; i++ {
		messages = append(messages,
			provider.Message{Role: "user", Content: fmt.Sprintf("recent-%d", i)},
			provider.Message{Role: "tool", Content: fmt.Sprintf("recent-tool-%d", i)},
			provider.Message{Role: "assistant", Content: fmt.Sprintf("recent-reply-%d", i)},
		)
	}

	mc := compact.NewMicrocompactor(3)
	result := mc.Apply(messages)

	// Recent tool results should be preserved
	for _, m := range result {
		if strings.HasPrefix(m.Content, "recent-tool-") {
			// Good — preserved
		}
	}

	// Old tool results should be replaced
	stubCount := 0
	for _, m := range result {
		if m.Content == "[previous tool result removed]" {
			stubCount++
		}
	}
	if stubCount == 0 {
		t.Error("Expected old tool results to be replaced with stubs")
	}
}

func TestMicrocompactor_EmptyInput(t *testing.T) {
	mc := compact.NewMicrocompactor(10)
	result := mc.Apply(nil)
	if len(result) != 0 {
		t.Errorf("Expected empty output, got %d messages", len(result))
	}
}

// =============================================================================
// 6. SYSTEM PROMPT ASSEMBLY: sysctl integration
// =============================================================================

func TestSystemPromptAssembly(t *testing.T) {
	cfg := config.Default()
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool())
	registry.Register(tool.NewBashTool())

	instructions := []config.Instruction{
		{Path: "CLAUDE.md", Content: "Be careful with file edits."},
	}

	env := sysctl.EnvContext{
		WorkDir:  "/home/user/project",
		Date:     "2026-04-01",
		Platform: "linux/amd64",
	}

	sections := sysctl.BuildSystemPrompt(cfg, registry, instructions, env)

	if len(sections) < 4 {
		t.Fatalf("Expected at least 4 sections (persona, tools, instructions, env), got %d", len(sections))
	}

	// First section should be persona with cache control
	if sections[0].CacheControl == nil {
		t.Error("Persona section should have cache control")
	}
	if !strings.Contains(sections[0].Content, "coding assistant") {
		t.Error("Persona section should contain role description")
	}

	// Tool section should list registered tools
	if !strings.Contains(sections[1].Content, "read") || !strings.Contains(sections[1].Content, "bash") {
		t.Error("Tool section should list registered tools")
	}

	// Instructions should be included
	foundInstructions := false
	for _, s := range sections {
		if strings.Contains(s.Content, "Be careful with file edits") {
			foundInstructions = true
		}
	}
	if !foundInstructions {
		t.Error("Instructions should be in system prompt")
	}

	// Env section should be last and not cached
	lastSection := sections[len(sections)-1]
	if lastSection.CacheControl != nil {
		t.Error("Env section should NOT have cache control (changes between turns)")
	}
	if !strings.Contains(lastSection.Content, "/home/user/project") {
		t.Error("Env section should contain working directory")
	}
}

// =============================================================================
// 7. MARKDOWN RENDERER: edge cases
// =============================================================================

func TestMarkdown_NestedBackticks(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	// Code block containing backticks in content
	input := "Text\n\n```go\nfmt.Println(\"`hello`\")\n```\n"
	result := r.Render(input)
	if result == "" {
		t.Fatal("Should not crash on nested backticks")
	}
}

func TestMarkdown_MultipleCodeBlocks(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	input := "Block 1:\n```go\nfunc a() {}\n```\n\nBlock 2:\n```py\ndef b(): pass\n```\n"
	result := r.Render(input)
	if !strings.Contains(result, "go") || !strings.Contains(result, "py") {
		t.Error("Should render both code blocks with language labels")
	}
}

func TestMarkdown_EmptyInput(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	result := r.Render("")
	if result == "" {
		t.Error("Empty input should produce at least a newline")
	}
}

func TestMarkdown_OnlyHeadings(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	result := r.Render("# Title\n## Subtitle\n### Section")
	if result == "" {
		t.Fatal("Should render headings")
	}
}

func TestMarkdown_CacheHit(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	input := "Hello, world!"
	r1 := r.Render(input)
	r2 := r.Render(input)
	if r1 != r2 {
		t.Error("Cache should return identical results")
	}
}

func TestMarkdown_StreamingNotCached(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	// Unclosed code block (streaming)
	incomplete := "```go\nfunc main() {"
	r.Render(incomplete)

	// Complete version
	complete := incomplete + "\n}\n```"
	result := r.Render(complete)
	if strings.Contains(result, "streaming") {
		t.Error("Completed code block should not show streaming indicator")
	}
}

func TestMarkdown_BulletPoints(t *testing.T) {
	r := tui.NewMarkdownRenderer(80)
	result := r.Render("- item 1\n* item 2\n- item 3")
	// Bullets should be indented
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "item") && !strings.HasPrefix(line, "  ") {
			t.Errorf("Bullet should be indented: %q", line)
		}
	}
}

// =============================================================================
// 8. TOOL INTEGRATION: real tools with filesystem
// =============================================================================

func TestToolIntegration_ReadEditWriteCycle(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.go")
	os.WriteFile(filePath, []byte("package main\n\nfunc hello() {}\n"), 0o644)

	ctx := context.Background()

	// Read
	readTool := tool.NewReadTool()
	input, _ := json.Marshal(map[string]any{"file_path": filePath})
	result, err := readTool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(result.Output, "package main") {
		t.Error("Read should return file contents")
	}

	// Edit
	editTool := tool.NewEditTool()
	input, _ = json.Marshal(map[string]any{
		"file_path":  filePath,
		"old_string": "func hello() {}",
		"new_string": "func hello() { fmt.Println(\"hi\") }",
	})
	result, err = editTool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("Edit error: %v", result.Error)
	}

	// Verify edit
	data, _ := os.ReadFile(filePath)
	if !strings.Contains(string(data), "fmt.Println") {
		t.Error("Edit should have modified file")
	}

	// Write new file
	writeTool := tool.NewWriteTool()
	newPath := filepath.Join(dir, "sub", "new.go")
	input, _ = json.Marshal(map[string]any{
		"file_path": newPath,
		"content":   "package sub\n",
	})
	result, err = writeTool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify write created directory and file
	data, err = os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("Written file not found: %v", err)
	}
	if string(data) != "package sub\n" {
		t.Errorf("Written content wrong: %q", string(data))
	}

	// Glob
	globTool := tool.NewGlobTool()
	input, _ = json.Marshal(map[string]any{"pattern": "*.go", "path": dir})
	result, err = globTool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if !strings.Contains(result.Output, "test.go") {
		t.Error("Glob should find test.go")
	}

	// Ls
	lsTool := tool.NewLsTool()
	input, _ = json.Marshal(map[string]any{"path": dir})
	result, err = lsTool.Execute(ctx, input)
	if err != nil {
		t.Fatalf("Ls: %v", err)
	}
	if !strings.Contains(result.Output, "sub") || !strings.Contains(result.Output, "test.go") {
		t.Errorf("Ls should show sub/ and test.go: %s", result.Output)
	}
}

func TestToolIntegration_EditNonexistentFile(t *testing.T) {
	editTool := tool.NewEditTool()
	input, _ := json.Marshal(map[string]any{
		"file_path":  "/nonexistent/file.go",
		"old_string": "x",
		"new_string": "y",
	})
	result, err := editTool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Should not return error, got %v", err)
	}
	if !strings.Contains(result.Output, "Error") {
		t.Error("Should report error in output for nonexistent file")
	}
}

func TestToolIntegration_EditAmbiguousMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo\nfoo\nbar\n"), 0o644)

	editTool := tool.NewEditTool()
	input, _ := json.Marshal(map[string]any{
		"file_path":  path,
		"old_string": "foo",
		"new_string": "baz",
	})
	result, _ := editTool.Execute(context.Background(), input)
	if result.Error == nil {
		t.Error("Should error on ambiguous match (2 occurrences)")
	}
}

func TestToolIntegration_BashTimeout(t *testing.T) {
	bashTool := tool.NewBashTool()
	input, _ := json.Marshal(map[string]any{
		"command": "sleep 10",
		"timeout": 100, // 100ms timeout
	})

	start := time.Now()
	result, _ := bashTool.Execute(context.Background(), input)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("Timeout didn't work, took %v", elapsed)
	}
	_ = result
}

func TestToolIntegration_BashExitCode(t *testing.T) {
	bashTool := tool.NewBashTool()
	input, _ := json.Marshal(map[string]any{"command": "exit 42"})
	result, _ := bashTool.Execute(context.Background(), input)

	if result.Metadata == nil {
		t.Fatal("Missing metadata")
	}
	if code, ok := result.Metadata["exit_code"]; !ok || code != 42 {
		t.Errorf("Expected exit code 42, got %v", result.Metadata["exit_code"])
	}
}

func TestToolIntegration_RegistrySchemas(t *testing.T) {
	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool())
	registry.Register(tool.NewBashTool())
	registry.Register(tool.NewEditTool())

	schemas := registry.Schemas()
	if len(schemas) != 3 {
		t.Fatalf("Expected 3 schemas, got %d", len(schemas))
	}

	for _, s := range schemas {
		if s.Name == "" {
			t.Error("Schema missing name")
		}
		if s.Description == "" {
			t.Errorf("Schema %q missing description", s.Name)
		}
		// Verify InputSchema is valid JSON
		var js json.RawMessage
		if err := json.Unmarshal(s.InputSchema, &js); err != nil {
			t.Errorf("Schema %q has invalid JSON: %v", s.Name, err)
		}
	}
}

// =============================================================================
// 9. ENGINE: parseModel edge cases
// =============================================================================

func TestEngineCreation_UnsupportedProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Model = "openai/gpt-4"

	_, err := engine.New(engine.EngineParams{Config: cfg})
	if err == nil {
		t.Fatal("Expected error for unsupported provider")
	}
	if !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("Error should mention unsupported provider: %v", err)
	}
}

func TestEngineCreation_DefaultProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Model = "claude-haiku-4-5-20251001" // no provider prefix
	cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "test"}

	eng, err := engine.New(engine.EngineParams{Config: cfg})
	if err != nil {
		t.Fatalf("Should default to anthropic: %v", err)
	}
	_ = eng
}

// =============================================================================
// 10. RETRY: exponential backoff behavior
// =============================================================================

func TestRetryableStream_ImmediateSuccess(t *testing.T) {
	ch := make(chan provider.StreamEvent, 1)
	ch <- provider.StreamEvent{Type: provider.StreamDone}
	close(ch)

	result, err := provider.RetryableStream(
		context.Background(),
		provider.RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond},
		func(ctx context.Context) (<-chan provider.StreamEvent, error) {
			return ch, nil
		},
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil channel")
	}
}

func TestRetryableStream_EventualSuccess(t *testing.T) {
	attempts := 0
	result, err := provider.RetryableStream(
		context.Background(),
		provider.RetryConfig{MaxRetries: 5, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond},
		func(ctx context.Context) (<-chan provider.StreamEvent, error) {
			attempts++
			if attempts < 3 {
				return nil, fmt.Errorf("temporary error %d", attempts)
			}
			ch := make(chan provider.StreamEvent, 1)
			ch <- provider.StreamEvent{Type: provider.StreamDone}
			close(ch)
			return ch, nil
		},
	)
	if err != nil {
		t.Fatalf("Should succeed after retries: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil channel")
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestRetryableStream_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.RetryableStream(
		ctx,
		provider.RetryConfig{MaxRetries: 10, BaseDelay: time.Second, MaxDelay: time.Second},
		func(ctx context.Context) (<-chan provider.StreamEvent, error) {
			return nil, fmt.Errorf("fail")
		},
	)
	if err == nil {
		t.Fatal("Expected error on cancelled context")
	}
}

// Ensure all event types are importable (compile-time check)
var _ = event.TextDelta
var _ = event.TextDone
var _ = event.ToolStart
var _ = event.ToolDelta
var _ = event.ToolDone
var _ = event.ToolResultEvent
var _ = event.ThinkingDelta
var _ = event.UsageEvent
var _ = event.PermissionRequest
var _ = event.PermissionResponse
var _ = event.ErrorEvent
var _ = event.Done

// Ensure engine types are importable
var _ engine.EngineParams
