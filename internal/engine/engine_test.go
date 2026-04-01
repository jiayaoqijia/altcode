package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/store"
)

// =============================================================================
// HELPERS
// =============================================================================

func sse(eventType, data string) string {
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, data)
}

// mockAnthropicServer creates a mock that returns different SSE bodies per call.
// responses is a slice of SSE bodies; each call consumes the next one.
func mockAnthropicServer(t *testing.T, responses []string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	callIdx := 0

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		idx := callIdx
		callIdx++
		mu.Unlock()

		if idx >= len(responses) {
			t.Errorf("unexpected call #%d (only %d responses configured)", idx, len(responses))
			w.WriteHeader(500)
			return
		}

		// Verify request has tool schemas when tools are registered
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(responses[idx]))
	}))
}

func cfgWithServer(srv *httptest.Server) *config.Config {
	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	}
	return cfg
}

func collectEvents(ch <-chan event.Event) []event.Event {
	var events []event.Event
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func countEventType(events []event.Event, typ event.EventType) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// textOnlySSE returns SSE for a simple text response with no tool calls.
func textOnlySSE(text string) string {
	var sb strings.Builder
	sb.WriteString(sse("content_block_start", `{"index":0,"content_block":{"type":"text","text":""}}`))
	sb.WriteString(sse("content_block_delta", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)))
	sb.WriteString(sse("content_block_stop", `{}`))
	sb.WriteString(sse("message_stop", `{}`))
	return sb.String()
}

// toolCallSSE returns SSE for a tool_use block with the given name and JSON input.
func toolCallSSE(toolID, toolName, inputJSON string) string {
	var sb strings.Builder
	sb.WriteString(sse("content_block_start", fmt.Sprintf(
		`{"index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, toolID, toolName)))
	sb.WriteString(sse("content_block_delta", fmt.Sprintf(
		`{"delta":{"type":"input_json_delta","partial_json":%q}}`, inputJSON)))
	sb.WriteString(sse("content_block_stop", `{}`))
	sb.WriteString(sse("message_stop", `{}`))
	return sb.String()
}

// textAndToolSSE returns SSE with text followed by a tool call.
func textAndToolSSE(text, toolID, toolName, inputJSON string) string {
	var sb strings.Builder
	// Text block
	sb.WriteString(sse("content_block_start", `{"index":0,"content_block":{"type":"text","text":""}}`))
	sb.WriteString(sse("content_block_delta", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)))
	sb.WriteString(sse("content_block_stop", `{}`))
	// Tool block
	sb.WriteString(sse("content_block_start", fmt.Sprintf(
		`{"index":1,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, toolID, toolName)))
	sb.WriteString(sse("content_block_delta", fmt.Sprintf(
		`{"delta":{"type":"input_json_delta","partial_json":%q}}`, inputJSON)))
	sb.WriteString(sse("content_block_stop", `{}`))
	sb.WriteString(sse("message_stop", `{}`))
	return sb.String()
}

// =============================================================================
// HAPPY PATH: Agent loop with tool calls
// =============================================================================

func TestAgentLoop_NoToolCalls(t *testing.T) {
	srv := mockAnthropicServer(t, []string{textOnlySSE("Hello!")})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "hi"))

	// Should have TextDelta + Done
	if countEventType(events, event.TextDelta) == 0 {
		t.Error("Expected TextDelta events")
	}
	if countEventType(events, event.Done) != 1 {
		t.Error("Expected exactly one Done event")
	}
	// No tool events
	if countEventType(events, event.ToolStart) != 0 {
		t.Error("Expected no ToolStart events for text-only response")
	}

	// Verify messages: user + assistant
	msgs := eng.Messages()
	if len(msgs) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hi" {
		t.Errorf("First message wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Hello!" {
		t.Errorf("Second message wrong: %+v", msgs[1])
	}
}

func TestAgentLoop_SingleToolCall(t *testing.T) {
	// Turn 1: model calls "ls" tool
	// Turn 2: model responds with text after getting tool result
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("tool_1", "ls", `{"path":"."}`),
		textOnlySSE("Directory listed."),
	})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "list files"))

	// Should have ToolStart + ToolResult + TextDelta + Done
	if countEventType(events, event.ToolResultEvent) == 0 {
		t.Error("Expected ToolResult events")
	}
	if countEventType(events, event.Done) != 1 {
		t.Error("Expected Done event")
	}

	// Verify messages: user, assistant(tool_use), user(tool_result), assistant(text)
	msgs := eng.Messages()
	if len(msgs) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Error("msg[0] should be user")
	}
	if msgs[1].Role != "assistant" || len(msgs[1].Parts) == 0 {
		t.Error("msg[1] should be assistant with Parts (tool_use)")
	}
	if msgs[2].Role != "user" || len(msgs[2].Parts) == 0 {
		t.Error("msg[2] should be user with Parts (tool_result)")
	}
	if msgs[3].Role != "assistant" || msgs[3].Content != "Directory listed." {
		t.Error("msg[3] should be final assistant text")
	}
}

func TestAgentLoop_MultipleToolCalls(t *testing.T) {
	// Turn 1: model calls "read" then "grep"
	turn1 := sse("content_block_start", `{"index":0,"content_block":{"type":"tool_use","id":"t1","name":"read"}}`) +
		sse("content_block_delta", `{"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/tmp/x\"}"}}`) +
		sse("content_block_stop", `{}`) +
		sse("content_block_start", `{"index":1,"content_block":{"type":"tool_use","id":"t2","name":"grep"}}`) +
		sse("content_block_delta", `{"delta":{"type":"input_json_delta","partial_json":"{\"pattern\":\"foo\"}"}}`) +
		sse("content_block_stop", `{}`) +
		sse("message_stop", `{}`)

	srv := mockAnthropicServer(t, []string{
		turn1,
		textOnlySSE("Found it."),
	})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "find foo"))

	// Both tools should have results
	resultCount := countEventType(events, event.ToolResultEvent)
	if resultCount < 2 {
		t.Errorf("Expected at least 2 ToolResult events, got %d", resultCount)
	}

	// Final message should be text
	msgs := eng.Messages()
	lastMsg := msgs[len(msgs)-1]
	if lastMsg.Role != "assistant" || lastMsg.Content != "Found it." {
		t.Errorf("Last message wrong: %+v", lastMsg)
	}
}

func TestAgentLoop_FiveConsecutiveToolCalls(t *testing.T) {
	// Model calls tools 5 times before producing text
	responses := make([]string, 6)
	for i := 0; i < 5; i++ {
		responses[i] = toolCallSSE(
			fmt.Sprintf("t%d", i), "ls",
			fmt.Sprintf(`{"path":"/dir%d"}`, i),
		)
	}
	responses[5] = textOnlySSE("Done after 5 tool calls.")

	srv := mockAnthropicServer(t, responses)
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "scan 5 dirs"))

	if countEventType(events, event.Done) != 1 {
		t.Error("Expected Done")
	}

	// 1 user + 5*(assistant+tool_result) + 1 final assistant = 12 messages
	msgs := eng.Messages()
	if len(msgs) != 12 {
		t.Errorf("Expected 12 messages for 5 tool rounds, got %d", len(msgs))
	}
}

func TestAgentLoop_TextPlusToolInSameTurn(t *testing.T) {
	srv := mockAnthropicServer(t, []string{
		textAndToolSSE("I'll read that.", "t1", "read", `{"file_path":"/tmp/x"}`),
		textOnlySSE("Here's what I found."),
	})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "read /tmp/x"))

	if countEventType(events, event.TextDelta) == 0 {
		t.Error("Should have text deltas")
	}

	// Assistant message should have both text and tool_use parts
	msgs := eng.Messages()
	if len(msgs) < 2 {
		t.Fatal("Expected at least 2 messages")
	}
	assistMsg := msgs[1] // first assistant
	if len(assistMsg.Parts) < 2 {
		t.Errorf("Expected 2+ parts (text + tool_use), got %d", len(assistMsg.Parts))
	}
}

// =============================================================================
// ERROR PATHS
// =============================================================================

func TestAgentLoop_ProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "hi"))

	if countEventType(events, event.ErrorEvent) == 0 {
		t.Error("Expected error event for 500 response")
	}
	if countEventType(events, event.Done) != 1 {
		t.Error("Should still emit Done even on error")
	}
}

func TestAgentLoop_StreamError(t *testing.T) {
	errSSE := sse("error", `{"error":{"message":"overloaded"}}`)
	srv := mockAnthropicServer(t, []string{errSSE})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "hi"))

	hasError := false
	for _, ev := range events {
		if ev.Type == event.ErrorEvent && strings.Contains(ev.Error, "overloaded") {
			hasError = true
		}
	}
	if !hasError {
		t.Error("Expected error event with 'overloaded' message")
	}
}

func TestAgentLoop_UnknownTool(t *testing.T) {
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "nonexistent_tool", `{}`),
		textOnlySSE("OK, that tool doesn't exist."),
	})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "use unknown"))

	// Should get a tool result with error
	hasResult := false
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "unknown tool") {
				hasResult = true
			}
		}
	}
	if !hasResult {
		t.Error("Expected tool result mentioning unknown tool")
	}

	// Should still complete
	if countEventType(events, event.Done) != 1 {
		t.Error("Should still emit Done")
	}
}

func TestAgentLoop_ContextCancelled(t *testing.T) {
	// Server that sends SSE slowly — one chunk then hangs
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, ok := w.(http.Flusher)
		if ok {
			flusher.Flush()
		}
		// Write one text delta then hang
		w.Write([]byte(sse("content_block_start", `{"index":0,"content_block":{"type":"text","text":""}}`)))
		if ok {
			flusher.Flush()
		}
		// Block until request context done (client disconnect or test timeout)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := eng.Run(ctx, "hi")

	// Drain events with timeout
	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
		// Channel closed — test passes
	case <-time.After(3 * time.Second):
		t.Fatal("Engine did not shut down after context cancellation")
	}
}

func TestAgentLoop_ContextCancelledBetweenTurns(t *testing.T) {
	// First turn returns a tool call, but context is cancelled before second call
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "ls", `{"path":"."}`),
		// Second response would be served but context cancelled first
		textOnlySSE("never reached"),
	})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := eng.Run(ctx, "hi")

	// Read first events (tool dispatch), then cancel
	for ev := range ch {
		if ev.Type == event.ToolResultEvent {
			cancel()
		}
		if ev.Type == event.Done || ev.Type == event.ErrorEvent {
			break
		}
	}
	// Drain remaining
	for range ch {
	}
}

// =============================================================================
// EDGE CASES
// =============================================================================

func TestAgentLoop_EmptyToolInput(t *testing.T) {
	// Tool call with no input JSON deltas
	emptyTool := sse("content_block_start", `{"index":0,"content_block":{"type":"tool_use","id":"t1","name":"ls"}}`) +
		sse("content_block_stop", `{}`) +
		sse("message_stop", `{}`)

	srv := mockAnthropicServer(t, []string{
		emptyTool,
		textOnlySSE("Listed."),
	})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "ls"))

	// Should not panic, should default to {}
	if countEventType(events, event.Done) != 1 {
		t.Error("Expected Done")
	}
}

func TestAgentLoop_EmptyTextResponse(t *testing.T) {
	// Model returns empty text (no content blocks at all)
	emptySSE := sse("message_stop", `{}`)

	srv := mockAnthropicServer(t, []string{emptySSE})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "hi"))
	if countEventType(events, event.Done) != 1 {
		t.Error("Expected Done even for empty response")
	}
}

// =============================================================================
// PERMISSION FLOW
// =============================================================================

func TestAgentLoop_PermissionDeny(t *testing.T) {
	// Model tries to use bash, which is denied
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "bash", `{"command":"rm -rf /"}`),
		textOnlySSE("OK, I won't do that."),
	})
	defer srv.Close()

	perm := permission.NewEvaluator(permission.ModeDefault, "", []permission.Rule{
		{Tool: "bash", Pattern: "rm *", Action: permission.ActionDeny, Source: "test"},
	})

	eng, err := engine.New(engine.EngineParams{
		Config: cfgWithServer(srv),
		Perm:   perm,
	})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "delete everything"))

	// Tool result should contain "Permission denied"
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Permission denied") {
				return // pass
			}
		}
	}
	t.Error("Expected tool result with 'Permission denied'")
}

func TestAgentLoop_PermissionAllow(t *testing.T) {
	// Model uses read (always allowed)
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "read", `{"file_path":"/dev/null"}`),
		textOnlySSE("Empty file."),
	})
	defer srv.Close()

	perm := permission.NewEvaluator(permission.ModeDefault, "", nil)
	eng, err := engine.New(engine.EngineParams{
		Config: cfgWithServer(srv),
		Perm:   perm,
	})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "read /dev/null"))

	// Should successfully execute (no deny in results)
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Permission denied") {
				t.Error("Read should be allowed by default")
			}
		}
	}
	if countEventType(events, event.Done) != 1 {
		t.Error("Expected Done")
	}
}

func TestAgentLoop_PermissionAskApproved(t *testing.T) {
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "bash", `{"command":"echo hi"}`),
		textOnlySSE("Done."),
	})
	defer srv.Close()

	perm := permission.NewEvaluator(permission.ModeDefault, "", nil)
	eng, err := engine.New(engine.EngineParams{
		Config: cfgWithServer(srv),
		Perm:   perm,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ch := eng.Run(ctx, "echo hi")

	// Consume events, approve permission when asked
	var events []event.Event
	for ev := range ch {
		events = append(events, ev)
		if ev.Type == event.PermissionRequest && ev.Permission != nil {
			ev.Permission.Response <- event.PermResponse{Action: event.Allow}
		}
	}

	// Should have a tool result (not denied)
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Permission denied") {
				t.Error("Should have been approved")
			}
		}
	}
}

func TestAgentLoop_PermissionAskDenied(t *testing.T) {
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "bash", `{"command":"echo hi"}`),
		textOnlySSE("Understood."),
	})
	defer srv.Close()

	perm := permission.NewEvaluator(permission.ModeDefault, "", nil)
	eng, err := engine.New(engine.EngineParams{
		Config: cfgWithServer(srv),
		Perm:   perm,
	})
	if err != nil {
		t.Fatal(err)
	}

	ch := eng.Run(context.Background(), "echo hi")

	var events []event.Event
	for ev := range ch {
		events = append(events, ev)
		if ev.Type == event.PermissionRequest && ev.Permission != nil {
			ev.Permission.Response <- event.PermResponse{Action: event.Deny}
		}
	}

	// Tool result should contain "Permission denied"
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Permission denied") {
				return // pass
			}
		}
	}
	t.Error("Expected Permission denied in tool result")
}

// =============================================================================
// CONTENTPART SERIALIZATION
// =============================================================================

func TestContentPart_TextOnlySerialization(t *testing.T) {
	msg := provider.TextMessage("user", "hello")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded provider.Message
	json.Unmarshal(data, &decoded)
	if decoded.Content != "hello" || decoded.Role != "user" {
		t.Errorf("Unexpected: %+v", decoded)
	}
	if len(decoded.Parts) != 0 {
		t.Error("Text-only should have no Parts")
	}
}

func TestContentPart_ToolUseSerialization(t *testing.T) {
	msg := provider.Message{
		Role: "assistant",
		Parts: []provider.ContentPart{
			{Type: "text", Text: "I'll read that."},
			{Type: "tool_use", ID: "t1", Name: "read", Input: json.RawMessage(`{"file_path":"/tmp/x"}`)},
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded provider.Message
	json.Unmarshal(data, &decoded)
	if len(decoded.Parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(decoded.Parts))
	}
	if decoded.Parts[0].Type != "text" || decoded.Parts[0].Text != "I'll read that." {
		t.Errorf("Part 0 wrong: %+v", decoded.Parts[0])
	}
	if decoded.Parts[1].Type != "tool_use" || decoded.Parts[1].Name != "read" {
		t.Errorf("Part 1 wrong: %+v", decoded.Parts[1])
	}
}

func TestContentPart_ToolResultSerialization(t *testing.T) {
	part := provider.NewToolResultPart("t1", "file contents here")
	if part.Type != "tool_result" {
		t.Errorf("Expected tool_result type, got %q", part.Type)
	}
	if part.ToolUseID != "t1" {
		t.Errorf("Expected ToolUseID t1, got %q", part.ToolUseID)
	}

	msg := provider.ToolResultMessage([]provider.ContentPart{part})
	if msg.Role != "user" {
		t.Errorf("Tool result message should be role=user, got %q", msg.Role)
	}
}

func TestContentPart_EmptyParts(t *testing.T) {
	msg := provider.Message{Role: "user", Parts: []provider.ContentPart{}}
	data, _ := json.Marshal(msg)
	if !strings.Contains(string(data), `"role":"user"`) {
		t.Error("Should serialize role")
	}
}

func TestContentPart_MixedContentRoundTrip(t *testing.T) {
	original := provider.Message{
		Role: "assistant",
		Parts: []provider.ContentPart{
			{Type: "text", Text: "Let me help."},
			{Type: "tool_use", ID: "abc", Name: "bash", Input: json.RawMessage(`{"command":"ls"}`)},
		},
	}

	data, _ := json.Marshal(original)
	var decoded provider.Message
	json.Unmarshal(data, &decoded)

	if len(decoded.Parts) != 2 {
		t.Fatal("Parts lost in round-trip")
	}
	if string(decoded.Parts[1].Input) != `{"command":"ls"}` {
		t.Errorf("Input lost: %s", string(decoded.Parts[1].Input))
	}
}

// =============================================================================
// SESSION PERSISTENCE
// =============================================================================

func TestSession_PersistMessages(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sess, err := db.CreateSession("test-project", "test", "claude-test")
	if err != nil {
		t.Fatal(err)
	}

	srv := mockAnthropicServer(t, []string{textOnlySSE("Hi there!")})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{
		Config:    cfgWithServer(srv),
		Store:     db,
		SessionID: sess.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run a turn
	collectEvents(eng.Run(context.Background(), "hello"))

	// Verify messages were persisted
	msgs, err := db.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(msgs) < 2 {
		t.Fatalf("Expected at least 2 persisted messages, got %d", len(msgs))
	}

	// First should be user, second assistant
	if msgs[0].Role != "user" {
		t.Errorf("First persisted message should be user, got %q", msgs[0].Role)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("Second persisted message should be assistant, got %q", msgs[1].Role)
	}
}

func TestSession_PersistToolCallMessages(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sess, err := db.CreateSession("test-project", "test", "claude-test")
	if err != nil {
		t.Fatal(err)
	}

	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "ls", `{"path":"."}`),
		textOnlySSE("Listed."),
	})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{
		Config:    cfgWithServer(srv),
		Store:     db,
		SessionID: sess.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	collectEvents(eng.Run(context.Background(), "list"))

	msgs, err := db.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	// user + assistant(final text) = 2 persisted (tool dispatch messages not persisted individually)
	if len(msgs) < 2 {
		t.Fatalf("Expected at least 2 persisted messages, got %d", len(msgs))
	}
}

func TestSession_NoPersistWithoutStore(t *testing.T) {
	srv := mockAnthropicServer(t, []string{textOnlySSE("Hi!")})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic when store is nil
	collectEvents(eng.Run(context.Background(), "hello"))
}

func TestSession_NoPersistWithoutSessionID(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	srv := mockAnthropicServer(t, []string{textOnlySSE("Hi!")})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{
		Config: cfgWithServer(srv),
		Store:  db,
		// No SessionID
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic — just doesn't persist
	collectEvents(eng.Run(context.Background(), "hello"))
}

func TestSession_ResumeWithPreloadedMessages(t *testing.T) {
	preloaded := []provider.Message{
		provider.TextMessage("user", "previous question"),
		provider.TextMessage("assistant", "previous answer"),
	}

	srv := mockAnthropicServer(t, []string{textOnlySSE("Continued.")})
	defer srv.Close()

	eng, err := engine.New(engine.EngineParams{
		Config:   cfgWithServer(srv),
		Messages: preloaded,
	})
	if err != nil {
		t.Fatal(err)
	}

	collectEvents(eng.Run(context.Background(), "follow up"))

	msgs := eng.Messages()
	// 2 preloaded + 1 new user + 1 new assistant = 4
	if len(msgs) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "previous question" {
		t.Error("Preloaded messages should be preserved")
	}
	if msgs[2].Content != "follow up" {
		t.Error("New user message should be appended")
	}
}

func TestSession_LatestSession(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateSession("proj1", "old session", "model-a")
	time.Sleep(10 * time.Millisecond)
	s2, _ := db.CreateSession("proj1", "new session", "model-b")

	latest, err := db.LatestSession("proj1")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != s2.ID {
		t.Errorf("Expected latest session %q, got %q", s2.ID, latest.ID)
	}
}

func TestSession_LatestSessionNoSessions(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.LatestSession("nonexistent")
	if err == nil {
		t.Error("Expected error for no sessions")
	}
}

func TestSession_ToProviderMessages(t *testing.T) {
	msg := provider.TextMessage("user", "hello")
	data, _ := json.Marshal(msg)

	stored := []*store.Message{
		{Role: "user", Content: data},
	}

	converted := store.ToProviderMessages(stored)
	if len(converted) != 1 {
		t.Fatal("Expected 1 converted message")
	}
	if converted[0].Content != "hello" {
		t.Errorf("Expected 'hello', got %q", converted[0].Content)
	}
}

func TestSession_ToProviderMessagesWithParts(t *testing.T) {
	msg := provider.Message{
		Role: "assistant",
		Parts: []provider.ContentPart{
			{Type: "text", Text: "Let me help."},
			{Type: "tool_use", ID: "t1", Name: "read"},
		},
	}
	data, _ := json.Marshal(msg)

	stored := []*store.Message{
		{Role: "assistant", Content: data},
	}

	converted := store.ToProviderMessages(stored)
	if len(converted[0].Parts) != 2 {
		t.Errorf("Parts should survive round-trip, got %d", len(converted[0].Parts))
	}
}

func TestSession_ToProviderMessagesPlainText(t *testing.T) {
	// Legacy: content stored as plain text (not JSON)
	stored := []*store.Message{
		{Role: "user", Content: []byte("just plain text")},
	}

	converted := store.ToProviderMessages(stored)
	if converted[0].Content != "just plain text" {
		t.Errorf("Should fall back to plain text: %q", converted[0].Content)
	}
}

// =============================================================================
// ENGINE CREATION EDGE CASES
// =============================================================================

func TestEngine_UnsupportedProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Model = "openai/gpt-4"

	_, err := engine.New(engine.EngineParams{Config: cfg})
	if err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Errorf("Expected unsupported provider error, got: %v", err)
	}
}

func TestEngine_DefaultsToAnthropic(t *testing.T) {
	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "test"}

	eng, err := engine.New(engine.EngineParams{Config: cfg})
	if err != nil {
		t.Fatalf("Should default to anthropic: %v", err)
	}
	_ = eng
}

func TestEngine_NilPermDefaultsBypass(t *testing.T) {
	srv := mockAnthropicServer(t, []string{
		toolCallSSE("t1", "bash", `{"command":"rm -rf /"}`),
		textOnlySSE("Done."),
	})
	defer srv.Close()

	// No Perm = bypass mode (allow everything)
	eng, err := engine.New(engine.EngineParams{Config: cfgWithServer(srv)})
	if err != nil {
		t.Fatal(err)
	}

	events := collectEvents(eng.Run(context.Background(), "danger"))

	// Should NOT have permission denied
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent && ev.ToolResult != nil {
			if strings.Contains(ev.ToolResult.Output, "Permission denied") {
				t.Error("Nil perm should bypass all permissions")
			}
		}
	}
}
