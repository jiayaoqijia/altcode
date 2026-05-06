package tui

import (
	"strings"
	"testing"
	"time"
)

// TestTeamView_NewIsInactive verifies the constructor's zero state.
func TestTeamView_NewIsInactive(t *testing.T) {
	tv := newTeamView()
	if tv.IsActive() {
		t.Error("fresh teamView should not be active")
	}
	if !tv.AllDone() {
		t.Error("teamView with no panes should report AllDone (vacuously true)")
	}
}

// TestTeamView_StartCreatesPanes covers Start + the Run state.
func TestTeamView_StartCreatesPanes(t *testing.T) {
	tv := newTeamView()
	roles := []teamRole{
		{Role: "architect", Backend: "claude", Model: "claude-sonnet-4-6"},
		{Role: "coder", Backend: "codex", Model: "gpt-5.4"},
	}
	tv.Start(roles)

	if !tv.IsActive() {
		t.Error("Start() did not activate the teamView")
	}
	if len(tv.panes) != 2 {
		t.Fatalf("got %d panes, want 2", len(tv.panes))
	}
	for i, want := range roles {
		got := tv.panes[i]
		if got.Role != want.Role || got.Backend != want.Backend || got.Model != want.Model {
			t.Errorf("pane[%d] = %+v, want role=%s backend=%s", i, got, want.Role, want.Backend)
		}
		if got.Status != paneRunning {
			t.Errorf("pane[%d].Status = %v, want paneRunning", i, got.Status)
		}
	}
	if tv.AllDone() {
		t.Error("AllDone should be false while panes are running")
	}
}

// TestTeamView_AppendLine_RoutesToCorrectPane verifies output routing.
func TestTeamView_AppendLine_RoutesToCorrectPane(t *testing.T) {
	tv := newTeamView()
	tv.Start([]teamRole{{Role: "a"}, {Role: "b"}})
	tv.AppendLine("a", "from-a-1")
	tv.AppendLine("b", "from-b-1")
	tv.AppendLine("a", "from-a-2")
	tv.AppendLine("nonexistent", "ignored")

	if len(tv.panes[0].Lines) != 2 {
		t.Errorf("a got %d lines, want 2", len(tv.panes[0].Lines))
	}
	if len(tv.panes[1].Lines) != 1 {
		t.Errorf("b got %d lines, want 1", len(tv.panes[1].Lines))
	}
}

// TestTeamView_AppendLine_TrimsBufferAt50 covers the rolling-window
// guard that prevents unbounded growth from chatty agents.
func TestTeamView_AppendLine_TrimsBufferAt50(t *testing.T) {
	tv := newTeamView()
	tv.Start([]teamRole{{Role: "a"}})
	for i := 0; i < 60; i++ {
		tv.AppendLine("a", "line-"+strings.Repeat("x", i%4))
	}
	if got := len(tv.panes[0].Lines); got != 50 {
		t.Errorf("buffer = %d lines, want 50 (window cap)", got)
	}
}

// TestTeamView_MarkDone_SuccessAndFailure covers both branches of the
// status transition.
func TestTeamView_MarkDone_SuccessAndFailure(t *testing.T) {
	tv := newTeamView()
	tv.Start([]teamRole{{Role: "ok"}, {Role: "broken"}})

	tv.MarkDone("ok", 42*time.Millisecond, "")
	tv.MarkDone("broken", 100*time.Millisecond, "subprocess died")

	if tv.panes[0].Status != paneSucceeded {
		t.Errorf("ok.Status = %v, want paneSucceeded", tv.panes[0].Status)
	}
	if tv.panes[0].Elapsed != 42*time.Millisecond {
		t.Errorf("ok.Elapsed = %v, want 42ms", tv.panes[0].Elapsed)
	}
	if tv.panes[1].Status != paneFailed {
		t.Errorf("broken.Status = %v, want paneFailed", tv.panes[1].Status)
	}
	if tv.panes[1].Error != "subprocess died" {
		t.Errorf("broken.Error = %q", tv.panes[1].Error)
	}

	if !tv.AllDone() {
		t.Error("AllDone should be true after both panes finish")
	}
}

// TestTeamView_Stop deactivates without altering pane statuses.
func TestTeamView_Stop(t *testing.T) {
	tv := newTeamView()
	tv.Start([]teamRole{{Role: "a"}})
	tv.Stop()
	if tv.IsActive() {
		t.Error("Stop should deactivate")
	}
	// Pane status untouched.
	if tv.panes[0].Status != paneRunning {
		t.Errorf("Stop should not change pane status: %v", tv.panes[0].Status)
	}
}

// TestTeamView_SetSize stores width and height.
func TestTeamView_SetSize(t *testing.T) {
	tv := newTeamView()
	tv.SetSize(100, 30)
	if tv.width != 100 || tv.height != 30 {
		t.Errorf("size = (%d,%d), want (100,30)", tv.width, tv.height)
	}
}

// TestTeamView_Render_NoPanesReturnsEmpty covers the no-panes guard.
func TestTeamView_Render_NoPanesReturnsEmpty(t *testing.T) {
	tv := newTeamView()
	tv.SetSize(120, 30)
	if got := tv.Render(DefaultTheme); got != "" {
		t.Errorf("Render() with no panes = %q, want empty", got)
	}
}

// TestTeamView_Render_ShowsRolesAndBackends covers the happy path:
// each pane's role badge and backend label appear in the output.
func TestTeamView_Render_ShowsRolesAndBackends(t *testing.T) {
	tv := newTeamView()
	tv.SetSize(140, 20)
	tv.Start([]teamRole{
		{Role: "lead", Backend: "claude"},
		{Role: "coder", Backend: "codex"},
	})
	tv.AppendLine("lead", "planning the change")
	tv.AppendLine("coder", "writing tests")
	tv.MarkDone("lead", 50*time.Millisecond, "")
	tv.MarkDone("coder", 150*time.Millisecond, "")

	out := stripANSI(tv.Render(DefaultTheme))
	for _, want := range []string{"lead", "coder", "claude", "codex", "planning", "writing"} {
		if !strings.Contains(out, want) {
			t.Errorf("Render output missing %q in:\n%s", want, out)
		}
	}
}

// TestTeamView_Render_FailedPaneShowsError exercises the error-display
// branch in renderPane.
func TestTeamView_Render_FailedPaneShowsError(t *testing.T) {
	tv := newTeamView()
	tv.SetSize(140, 20)
	tv.Start([]teamRole{{Role: "lead", Backend: "claude"}})
	tv.MarkDone("lead", time.Second, "auth failed")

	out := stripANSI(tv.Render(DefaultTheme))
	if !strings.Contains(out, "auth failed") {
		t.Errorf("error text missing from render: %s", out)
	}
}

// TestTeamView_Render_TruncatesLongLines hits the rune-safe truncate
// branch in renderPane (lines longer than the pane width get "…"d).
func TestTeamView_Render_TruncatesLongLines(t *testing.T) {
	tv := newTeamView()
	tv.SetSize(60, 15) // narrow → small per-pane width
	tv.Start([]teamRole{{Role: "a"}, {Role: "b"}})
	tv.AppendLine("a", strings.Repeat("X", 200))
	out := stripANSI(tv.Render(DefaultTheme))
	if !strings.Contains(out, "...") {
		t.Errorf("expected '...' truncation marker:\n%s", out)
	}
}

// TestTeamView_Render_NarrowClampsPaneCount covers the n-decrement loop
// when terminal is too narrow for all panes.
func TestTeamView_Render_NarrowClampsPaneCount(t *testing.T) {
	tv := newTeamView()
	tv.SetSize(50, 15) // way too narrow for 4 panes at 20w each
	tv.Start([]teamRole{{Role: "a"}, {Role: "b"}, {Role: "c"}, {Role: "d"}})
	out := tv.Render(DefaultTheme)
	if out == "" {
		t.Error("narrow render should still produce output")
	}
}

// TestInterleave_Empty returns nil for an empty input.
func TestInterleave_Empty(t *testing.T) {
	if got := interleave(nil, "|"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestInterleave_Single returns the single item without separator.
func TestInterleave_Single(t *testing.T) {
	got := interleave([]string{"only"}, "|")
	if len(got) != 1 || got[0] != "only" {
		t.Errorf("got %v, want [only]", got)
	}
}

// TestInterleave_Multiple inserts the separator between every pair.
func TestInterleave_Multiple(t *testing.T) {
	got := interleave([]string{"a", "b", "c"}, "|")
	want := []string{"a", "|", "b", "|", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestTeamView_AllDone_PartiallyRunning returns false when at least one
// pane is still running.
func TestTeamView_AllDone_PartiallyRunning(t *testing.T) {
	tv := newTeamView()
	tv.Start([]teamRole{{Role: "a"}, {Role: "b"}})
	tv.MarkDone("a", time.Millisecond, "")
	if tv.AllDone() {
		t.Error("AllDone should be false while b is running")
	}
}
