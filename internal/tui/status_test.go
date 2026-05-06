package tui

import (
	"strings"
	"testing"
)

// TestStatusBar_NewStartsReady covers the constructor + idle render.
func TestStatusBar_NewStartsReady(t *testing.T) {
	s := NewStatusBar(DefaultTheme)
	s.SetWidth(80)
	out := stripANSI(s.View())
	if !strings.Contains(out, "ready") {
		t.Errorf("idle view missing 'ready': %q", out)
	}
	if !strings.Contains(out, "default") {
		t.Errorf("idle view missing default mode: %q", out)
	}
}

// TestStatusBar_BusyShowsSpinnerAndTool covers the busy branch.
func TestStatusBar_BusyShowsSpinnerAndTool(t *testing.T) {
	s := NewStatusBar(DefaultTheme)
	s.SetWidth(120)
	s.SetBusy(true)
	s.SetTool("write")
	out := stripANSI(s.View())
	if !strings.Contains(out, "write") {
		t.Errorf("busy view missing tool name: %q", out)
	}
	if strings.Contains(out, "ready") {
		t.Errorf("busy view should not say ready: %q", out)
	}
}

// TestStatusBar_AgentDepthShown covers the subagent indicator.
func TestStatusBar_AgentDepthShown(t *testing.T) {
	s := NewStatusBar(DefaultTheme)
	s.SetWidth(120)
	s.SetBusy(true)
	s.SetTool("dispatch")
	s.SetAgentDepth(2)
	out := stripANSI(s.View())
	if !strings.Contains(out, "agent[2]") {
		t.Errorf("missing agent depth indicator: %q", out)
	}
}

// TestStatusBar_ModeOverride flips the right-side label.
func TestStatusBar_ModeOverride(t *testing.T) {
	s := NewStatusBar(DefaultTheme)
	s.SetWidth(80)
	s.SetMode("plan")
	out := stripANSI(s.View())
	if !strings.Contains(out, "plan") {
		t.Errorf("missing mode override: %q", out)
	}
}

// TestStatusBar_NarrowGapClamped covers the gap-clamp branch (gap<1
// when the terminal is too narrow for the natural status spacing).
func TestStatusBar_NarrowGapClamped(t *testing.T) {
	s := NewStatusBar(DefaultTheme)
	s.SetWidth(20) // narrower than any reasonable status line
	out := s.View()
	if out == "" {
		t.Error("narrow width returned empty status (should still render, clamped)")
	}
}
