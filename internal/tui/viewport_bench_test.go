package tui

import (
	"fmt"
	"testing"
)

// BenchmarkUpdateViewport measures the per-call cost of updateViewport
// at growing conversation sizes. The current implementation iterates
// every message on every event — quadratic blowup once a.messages
// grows past a few hundred entries. Karpathy autoresearch metric:
// ns/op, lower is better.
func BenchmarkUpdateViewport(b *testing.B) {
	for _, n := range []int{10, 100, 500, 1000} {
		b.Run(fmt.Sprintf("messages-%d", n), func(b *testing.B) {
			app := testApp()
			// Pre-populate the conversation. Each message is a few
			// hundred bytes — representative of real usage where a
			// turn often spans several paragraphs of assistant text
			// + a tool tree result.
			for i := 0; i < n; i++ {
				app.messages = append(app.messages, chatMessage{
					role:    roleAssistant,
					content: fmt.Sprintf("turn %d: ", i) + msgPayload,
				})
			}
			app.viewport.Width = 100
			app.viewport.Height = 40
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				app.updateViewport()
			}
		})
	}
}

// 400-byte payload roughly matching real assistant message size.
const msgPayload = "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
	"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
	"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris " +
	"nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in " +
	"reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla " +
	"pariatur. Excepteur sint occaecat cupidatat non proident, sunt in " +
	"culpa qui officia deserunt mollit anim id est laborum."

// BenchmarkUpdateViewport_GrowingConversation simulates the realistic
// "every event appends one message" pattern. Without the prefix
// cache this is O(n²) in total work for an n-message conversation;
// with the cache it's O(n).
func BenchmarkUpdateViewport_GrowingConversation(b *testing.B) {
	app := testApp()
	app.viewport.Width = 100
	app.viewport.Height = 40
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.messages = append(app.messages, chatMessage{
			role:    roleAssistant,
			content: fmt.Sprintf("turn %d: ", i) + msgPayload,
		})
		app.updateViewport()
	}
}

// TestRenderCache_InvalidatesOnClear verifies the cache is dropped
// when messages are reset (e.g. by /clear), so a stale prefix can't
// leak into the next conversation.
func TestRenderCache_InvalidatesOnClear(t *testing.T) {
	app := testApp()
	app.viewport.Width = 100
	app.viewport.Height = 40
	app.messages = []chatMessage{
		{role: roleUser, content: "hello"},
		{role: roleAssistant, content: "world"},
	}
	app.updateViewport()
	if app.renderCacheLen != 2 {
		t.Fatalf("renderCacheLen = %d, want 2 after first paint",
			app.renderCacheLen)
	}

	// Simulate /clear.
	app.messages = nil
	app.updateViewport()
	if app.renderCacheLen != 0 {
		t.Errorf("renderCacheLen = %d after clear, want 0",
			app.renderCacheLen)
	}
	if app.renderCache != "" {
		t.Errorf("renderCache not reset after clear: %q",
			app.renderCache)
	}
}

// TestRenderCache_AppendOnlyExtends verifies the steady-state path:
// each new message appended re-uses the cached prefix.
func TestRenderCache_AppendOnlyExtends(t *testing.T) {
	app := testApp()
	app.viewport.Width = 100
	app.viewport.Height = 40
	for i := 0; i < 5; i++ {
		app.messages = append(app.messages, chatMessage{
			role:    roleAssistant,
			content: fmt.Sprintf("msg-%d", i),
		})
		app.updateViewport()
	}
	if app.renderCacheLen != 5 {
		t.Errorf("renderCacheLen = %d, want 5", app.renderCacheLen)
	}
	// Cache should contain all 5 message contents in order.
	for i := 0; i < 5; i++ {
		want := fmt.Sprintf("msg-%d", i)
		if !contains(app.renderCache, want) {
			t.Errorf("cache missing %q: %q", want, app.renderCache)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
