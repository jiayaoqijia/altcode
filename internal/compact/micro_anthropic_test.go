package compact_test

import (
	"testing"

	"github.com/jiayaoqijia/altcode/internal/compact"
	"github.com/jiayaoqijia/altcode/internal/provider"
)

// TestMicrocompactor_AnthropicToolResults verifies Anthropic-shaped
// tool results (role="user" with tool_result parts) get compacted
// alongside OpenAI-shaped (role="tool"). Codex round-H caught that
// the old role-string-only check silently no-oped on Anthropic.
func TestMicrocompactor_AnthropicToolResults(t *testing.T) {
	stub := "[previous tool result removed]"
	msgs := []provider.Message{
		provider.TextMessage("user", "turn 1 question"),
		{Role: "assistant", Parts: []provider.ContentPart{
			{Type: "tool_use", ID: "tu1", Name: "read"},
		}},
		// Anthropic-style: a role="user" message containing a
		// tool_result part (NOT a separate "tool" role).
		{Role: "user", Parts: []provider.ContentPart{
			{Type: "tool_result", ToolUseID: "tu1", Content: "LARGE PAYLOAD HERE"},
		}},
		provider.TextMessage("assistant", "turn 1 answer"),
		provider.TextMessage("user", "turn 2 question"),
		provider.TextMessage("assistant", "turn 2 answer"),
	}

	c := compact.NewMicrocompactor(1) // keep only last user turn
	out := c.Apply(msgs)
	if len(out) != len(msgs) {
		t.Fatalf("expected same length, got %d want %d", len(out), len(msgs))
	}

	// The Anthropic tool_result at index 2 should be replaced with stub.
	found := false
	for _, p := range out[2].Parts {
		if p.Type == "tool_result" && p.Content == stub {
			found = true
		}
		if p.Type == "tool_result" && p.Content == "LARGE PAYLOAD HERE" {
			t.Error("Anthropic tool_result leaked — compaction did not replace it")
		}
	}
	if !found {
		t.Error("expected tool_result stub in compacted message")
	}

	// Most recent user turn must remain intact.
	if out[len(out)-2].Content != "turn 2 question" {
		t.Errorf("recent user turn corrupted: %q", out[len(out)-2].Content)
	}
}

// TestMicrocompactor_PreservesNonToolResultUserMessages ensures the
// fix doesn't accidentally replace plain user messages.
func TestMicrocompactor_PreservesNonToolResultUserMessages(t *testing.T) {
	msgs := []provider.Message{
		provider.TextMessage("user", "first"),
		provider.TextMessage("assistant", "a"),
		provider.TextMessage("user", "second"),
		provider.TextMessage("assistant", "b"),
		provider.TextMessage("user", "third"),
	}
	c := compact.NewMicrocompactor(1)
	out := c.Apply(msgs)
	for i, m := range out {
		if m.Content != msgs[i].Content {
			t.Errorf("content at %d changed: %q -> %q",
				i, msgs[i].Content, m.Content)
		}
	}
}
