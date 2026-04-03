package event_test

import (
	"encoding/json"
	"testing"

	"github.com/altcode-ai/altcode/internal/event"
)

func TestEventTypes(t *testing.T) {
	// Verify all event type constants are distinct
	types := []event.EventType{
		event.TextDelta,
		event.TextDone,
		event.ToolStart,
		event.ToolDelta,
		event.ToolDone,
		event.ToolResultEvent,
		event.ThinkingDelta,
		event.UsageEvent,
		event.PermissionRequest,
		event.PermissionResponse,
		event.ErrorEvent,
		event.Done,
	}

	seen := map[event.EventType]bool{}
	for _, typ := range types {
		if seen[typ] {
			t.Errorf("Duplicate event type: %q", typ)
		}
		seen[typ] = true
		if string(typ) == "" {
			t.Error("Event type should not be empty string")
		}
	}
}

func TestEvent_JSONSerialization_TextDelta(t *testing.T) {
	ev := event.Event{
		Type: event.TextDelta,
		Text: "Hello world",
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var decoded event.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Type != event.TextDelta {
		t.Errorf("Type: %q", decoded.Type)
	}
	if decoded.Text != "Hello world" {
		t.Errorf("Text: %q", decoded.Text)
	}
}

func TestEvent_JSONSerialization_ToolCall(t *testing.T) {
	ev := event.Event{
		Type: event.ToolStart,
		ToolCall: &event.ToolCall{
			ID:   "t1",
			Name: "read",
			Input: json.RawMessage(`{"file_path":"/tmp/x"}`),
		},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var decoded event.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ToolCall == nil {
		t.Fatal("ToolCall should not be nil")
	}
	if decoded.ToolCall.ID != "t1" {
		t.Errorf("ID: %q", decoded.ToolCall.ID)
	}
	if decoded.ToolCall.Name != "read" {
		t.Errorf("Name: %q", decoded.ToolCall.Name)
	}
}

func TestEvent_JSONSerialization_ToolResult(t *testing.T) {
	ev := event.Event{
		Type: event.ToolResultEvent,
		ToolResult: &event.Result{
			Output: "file contents",
			Title:  "read /tmp/x",
		},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var decoded event.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ToolResult == nil {
		t.Fatal("ToolResult should not be nil")
	}
	if decoded.ToolResult.Output != "file contents" {
		t.Errorf("Output: %q", decoded.ToolResult.Output)
	}
}

func TestEvent_JSONSerialization_Error(t *testing.T) {
	ev := event.Event{
		Type:  event.ErrorEvent,
		Error: "something went wrong",
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var decoded event.Event
	json.Unmarshal(data, &decoded)

	if decoded.Error != "something went wrong" {
		t.Errorf("Error: %q", decoded.Error)
	}
}

func TestEvent_JSONSerialization_Usage(t *testing.T) {
	ev := event.Event{
		Type: event.UsageEvent,
		Usage: &event.UsageInfo{
			InputTokens:  100,
			OutputTokens: 50,
			CacheHits:    3,
		},
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var decoded event.Event
	json.Unmarshal(data, &decoded)

	if decoded.Usage == nil {
		t.Fatal("Usage should not be nil")
	}
	if decoded.Usage.InputTokens != 100 {
		t.Errorf("InputTokens: %d", decoded.Usage.InputTokens)
	}
	if decoded.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens: %d", decoded.Usage.OutputTokens)
	}
	if decoded.Usage.CacheHits != 3 {
		t.Errorf("CacheHits: %d", decoded.Usage.CacheHits)
	}
}

func TestEvent_JSONSerialization_Done(t *testing.T) {
	ev := event.Event{Type: event.Done}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var decoded event.Event
	json.Unmarshal(data, &decoded)
	if decoded.Type != event.Done {
		t.Errorf("Type: %q", decoded.Type)
	}
}

func TestEvent_OmitsEmptyFields(t *testing.T) {
	ev := event.Event{Type: event.Done}
	data, _ := json.Marshal(ev)

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	// Should only have "type", not empty fields
	if _, ok := m["text"]; ok {
		t.Error("Empty text should be omitted")
	}
	if _, ok := m["error"]; ok {
		t.Error("Empty error should be omitted")
	}
	if _, ok := m["tool_call"]; ok {
		t.Error("Nil tool_call should be omitted")
	}
}

func TestAction_Constants(t *testing.T) {
	if event.Allow != "allow" {
		t.Errorf("Allow: %q", event.Allow)
	}
	if event.Deny != "deny" {
		t.Errorf("Deny: %q", event.Deny)
	}
	if event.Ask != "ask" {
		t.Errorf("Ask: %q", event.Ask)
	}
}

func TestEvent_JSONSerialization_Thinking(t *testing.T) {
	ev := event.Event{
		Type:     event.ThinkingDelta,
		Thinking: "Let me consider...",
	}

	data, _ := json.Marshal(ev)
	var decoded event.Event
	json.Unmarshal(data, &decoded)

	if decoded.Thinking != "Let me consider..." {
		t.Errorf("Thinking: %q", decoded.Thinking)
	}
}
