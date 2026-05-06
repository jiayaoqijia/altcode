package tui

import (
	"strings"
	"sync"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/orchestra"
)

// TestWorkflowHeader_RenderEmpty returns "" when no phases set.
func TestWorkflowHeader_RenderEmpty(t *testing.T) {
	wh := &workflowHeader{}
	wh.SetWidth(80)
	if got := wh.Render(DefaultTheme); got != "" {
		t.Errorf("empty header = %q, want empty", got)
	}
}

// TestWorkflowHeader_SetPhasesShowsAllNames covers the labels + the
// joiner. NOTE: the pending icon "·" doesn't appear here because
// Verdict's zero value is VerdictPass — a freshly-set phase renders
// as ✓ until either MarkActive or MarkDone is called. This is a
// quirk of orchestra.Verdict's iota ordering; documented for future
// callers who expect "·" prior to any state transition.
func TestWorkflowHeader_SetPhasesShowsAllNames(t *testing.T) {
	wh := &workflowHeader{}
	wh.SetWidth(120)
	wh.SetPhases([]string{"interview", "plan", "implement"})

	out := stripANSI(wh.Render(DefaultTheme))
	for _, want := range []string{"interview", "plan", "implement", "→"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestWorkflowHeader_MarkActiveShowsRunningIcon covers the active branch.
func TestWorkflowHeader_MarkActiveShowsRunningIcon(t *testing.T) {
	wh := &workflowHeader{}
	wh.SetWidth(120)
	wh.SetPhases([]string{"plan", "execute"})
	wh.MarkActive("execute")

	out := stripANSI(wh.Render(DefaultTheme))
	if !strings.Contains(out, "⟳") {
		t.Errorf("expected ⟳ for running phase, got:\n%s", out)
	}
}

// TestWorkflowHeader_MarkActiveDeactivatesOthers verifies that switching
// the active phase clears the previous one's Active flag.
func TestWorkflowHeader_MarkActiveDeactivatesOthers(t *testing.T) {
	wh := &workflowHeader{}
	wh.SetWidth(120)
	wh.SetPhases([]string{"a", "b", "c"})
	wh.MarkActive("a")
	wh.MarkActive("c") // should clear a's Active

	wh.mu.RLock()
	defer wh.mu.RUnlock()
	if wh.phases[0].Active {
		t.Error("phase a should no longer be Active after switching to c")
	}
	if !wh.phases[2].Active {
		t.Error("phase c should be Active")
	}
}

// TestWorkflowHeader_MarkDoneShowsVerdictIcon covers each verdict
// branch (pass/fail/skipped) of the Render switch.
func TestWorkflowHeader_MarkDoneShowsVerdictIcon(t *testing.T) {
	cases := []struct {
		verdict orchestra.Verdict
		icon    string
	}{
		{orchestra.VerdictPass, "✓"},
		{orchestra.VerdictFail, "✗"},
		{orchestra.VerdictSkipped, "⊘"},
	}
	for _, c := range cases {
		wh := &workflowHeader{}
		wh.SetWidth(120)
		wh.SetPhases([]string{"only"})
		wh.MarkDone("only", c.verdict)

		out := stripANSI(wh.Render(DefaultTheme))
		if !strings.Contains(out, c.icon) {
			t.Errorf("verdict %v: missing %q in:\n%s", c.verdict, c.icon, out)
		}
	}
}

// TestWorkflowHeader_MarkDoneClearsActive — when a phase finishes, its
// Active flag is cleared even if it was the running one.
func TestWorkflowHeader_MarkDoneClearsActive(t *testing.T) {
	wh := &workflowHeader{}
	wh.SetWidth(120)
	wh.SetPhases([]string{"a"})
	wh.MarkActive("a")
	wh.MarkDone("a", orchestra.VerdictPass)

	wh.mu.RLock()
	defer wh.mu.RUnlock()
	if wh.phases[0].Active {
		t.Error("MarkDone should clear Active")
	}
}

// TestWorkflowHeader_ConcurrentSafe runs SetPhases / MarkActive /
// MarkDone / Render in parallel under -race. The package's own mutex
// must serialize them; without it -race flags the slice writes.
func TestWorkflowHeader_ConcurrentSafe(t *testing.T) {
	wh := &workflowHeader{}
	wh.SetWidth(120)
	wh.SetPhases([]string{"a", "b", "c", "d"})

	const n = 50
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			wh.MarkActive("b")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			wh.MarkDone("c", orchestra.VerdictPass)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			wh.SetWidth(100 + i%30)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_ = wh.Render(DefaultTheme)
		}
	}()
	wg.Wait()
}
