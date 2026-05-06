package tui

import (
	"strings"
	"testing"
)

// TestHeader_NewIsZeroState constructor returns a Header with no
// model/tokens until setters fire.
func TestHeader_NewIsZeroState(t *testing.T) {
	h := NewHeader(DefaultTheme)
	if h.model != "" || h.tokens != 0 || h.contextPct != 0 {
		t.Errorf("NewHeader = %+v, want zero state", h)
	}
}

// TestHeader_SettersWriteThrough verifies each setter mutates the
// expected field. (Not via View output to keep this isolated from the
// lipgloss rendering format.)
func TestHeader_SettersWriteThrough(t *testing.T) {
	h := NewHeader(DefaultTheme)
	h.SetModel("claude-sonnet-4-6")
	h.SetTokens(12345)
	h.SetContextPct(0.42)
	h.SetWidth(100)

	if h.model != "claude-sonnet-4-6" {
		t.Errorf("model = %q", h.model)
	}
	if h.tokens != 12345 {
		t.Errorf("tokens = %d", h.tokens)
	}
	if h.contextPct != 0.42 {
		t.Errorf("contextPct = %f", h.contextPct)
	}
	if h.width != 100 {
		t.Errorf("width = %d", h.width)
	}
}

// TestHeader_View_ContainsLogoAndModel covers the happy path: the
// rendered string includes the altcode logo, the model name, and the
// token/context info.
func TestHeader_View_ContainsLogoAndModel(t *testing.T) {
	h := NewHeader(DefaultTheme)
	h.SetWidth(120)
	h.SetModel("gpt-5.4")
	h.SetTokens(2500)
	h.SetContextPct(0.31)

	out := stripANSI(h.View())
	for _, want := range []string{"altcode", "gpt-5.4", "tokens: 2500", "context: 31%"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestHeader_View_NarrowGapClamped exercises the gap<1 clamp.
func TestHeader_View_NarrowGapClamped(t *testing.T) {
	h := NewHeader(DefaultTheme)
	h.SetWidth(20) // smaller than left+right combined
	h.SetModel("a-very-long-model-name-here")
	h.SetTokens(1000000)
	h.SetContextPct(0.99)
	out := h.View()
	if out == "" {
		t.Error("narrow header should still render (gap clamped)")
	}
}
