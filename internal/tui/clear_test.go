package tui

import (
	"testing"
)

// TestBuiltinClear_ResetsViewportPositioning is the regression for
// the bug where /clear after /help left the viewport stuck at the
// scrolled-down position and the freshly-cleared "Conversation cleared"
// info message rendered above the visible area.
func TestBuiltinClear_ResetsViewportPositioning(t *testing.T) {
	a := testApp()
	a.viewport.Width = 80
	a.viewport.Height = 20

	// Simulate the post-/help state: many messages, viewport scrolled.
	for i := 0; i < 50; i++ {
		a.messages = append(a.messages, chatMessage{
			role: roleAssistant, content: "long line of help text",
		})
	}
	a.updateViewport()
	a.viewport.LineDown(40) // scroll way down
	a.userScrolledAway = true

	a.builtinClear()

	// After clear: exactly one message (the "Conversation cleared." info),
	// viewport at top, scroll-away flag reset.
	if got := len(a.messages); got != 1 {
		t.Errorf("messages = %d, want 1 (just the cleared notice)", got)
	}
	if a.userScrolledAway {
		t.Error("userScrolledAway should be false after /clear")
	}
	if a.viewport.YOffset != 0 {
		t.Errorf("YOffset = %d, want 0", a.viewport.YOffset)
	}
}

// TestBuiltinClear_ResetsHUDCounters covers the previously-fixed HUD
// bug plus the two fields added with the cache-chip work
// (currentContextTokens, cachedTokens) — both should zero out on /clear.
func TestBuiltinClear_ResetsHUDCounters(t *testing.T) {
	a := testApp()
	a.tokensIn = 5000
	a.tokensOut = 1200
	a.currentContextTokens = 5000
	a.cachedTokens = 4000
	a.costUSD = 0.42
	a.toolCounts = map[string]int{"bash": 3, "read": 7}
	a.tools.Start("t1", "bash", "ls")

	a.builtinClear()

	if a.tokensIn != 0 || a.tokensOut != 0 {
		t.Errorf("tokens not reset: %d/%d", a.tokensIn, a.tokensOut)
	}
	if a.currentContextTokens != 0 {
		t.Errorf("currentContextTokens = %d, want 0", a.currentContextTokens)
	}
	if a.cachedTokens != 0 {
		t.Errorf("cachedTokens = %d, want 0", a.cachedTokens)
	}
	if a.costUSD != 0 {
		t.Errorf("cost not reset: %f", a.costUSD)
	}
	if len(a.toolCounts) != 0 {
		t.Errorf("toolCounts not cleared: %+v", a.toolCounts)
	}
	if len(a.tools.entries) != 0 {
		t.Errorf("tool tree not cleared: %d entries", len(a.tools.entries))
	}
}

// TestBuiltinClear_ResetsAutoAllowSeen ensures a fresh session resurfaces
// the [auto-allow] info note for the first tool it sees, instead of
// silently inheriting the previous session's suppression set.
func TestBuiltinClear_ResetsAutoAllowSeen(t *testing.T) {
	a := testApp()
	a.autoAllowSeen = map[string]bool{"bash": true, "write": true}

	a.builtinClear()

	if a.autoAllowSeen != nil {
		t.Errorf("autoAllowSeen should be nil after clear, got %+v", a.autoAllowSeen)
	}
}
