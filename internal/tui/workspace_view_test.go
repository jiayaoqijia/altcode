package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/jiayaoqijia/altcode/internal/workspace"
)

func TestWorkspaceView_Render(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01TEST",
		Task:   "add auth",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"architect": {
				Role:          "architect",
				Backend:       "claude",
				ActivityState: workspace.ActivityActive,
				TurnCount:     3,
			},
			"implementer": {
				Role:          "implementer",
				Backend:       "codex",
				ActivityState: workspace.ActivityReady,
				PRID:          42,
				CIStatus:      workspace.CIPass,
				TurnCount:     5,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(120, 30)
	wv.AppendAgentOutput("architect", "Reading codebase...")
	wv.AppendAgentOutput("implementer", "Writing tests...")

	output := wv.Render(DefaultTheme)
	if output == "" {
		t.Fatal("Render returned empty string")
	}

	// Check output is non-trivial (role names may be wrapped in ANSI codes)
	plain := stripANSI(output)
	if len(plain) < 20 {
		t.Errorf("Render output too short: %d chars", len(plain))
	}
	// Check agent output lines appear
	if !strings.Contains(plain, "Reading") {
		t.Error("missing agent output text in render")
	}
}

func TestWorkspaceView_NarrowTerminal(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01NARROW",
		Task:   "fix bug",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"fixer": {
				Role:          "fixer",
				Backend:       "claude",
				ActivityState: workspace.ActivityActive,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(40, 10) // very narrow
	output := wv.Render(DefaultTheme)
	if output == "" {
		t.Fatal("Render returned empty on narrow terminal")
	}
	// No line should exceed terminal width (accounting for ANSI codes is hard,
	// but raw lines without ANSI should be reasonable)
	for i, line := range strings.Split(output, "\n") {
		// Strip ANSI escape codes for width check
		plain := stripANSI(line)
		if len([]rune(plain)) > 45 { // 40 + some padding tolerance
			t.Errorf("line %d exceeds width: %d runes: %q", i, len([]rune(plain)), plain)
		}
	}
}

func TestWorkspaceView_VisualLanguageStillShowsFocusAndBlocked(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:         "01VISUAL",
		Task:       "refresh chat shell without regressing workspace",
		Status:     workspace.WSSWorking,
		BaseBranch: "main",
		Agents: map[string]*workspace.AgentRecord{
			"architect": {
				Role:          "architect",
				Backend:       "claude",
				ActivityState: workspace.ActivityExited,
				TurnCount:     2,
				Branch:        "altcode/01VISUAL/architect",
			},
			"implementer": {
				Role:          "implementer",
				Backend:       "codex",
				ActivityState: workspace.ActivityActive,
				TurnCount:     7,
				PRID:          17,
				CIStatus:      workspace.CIRunning,
				Branch:        "altcode/01VISUAL/implementer",
			},
			"reviewer": {
				Role:          "reviewer",
				Backend:       "claude",
				ActivityState: workspace.ActivityBlocked,
				TurnCount:     1,
				Branch:        "altcode/01VISUAL/reviewer",
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(120, 30)
	wv.FocusAgent(1)
	wv.AppendAgentOutput("implementer", "Running focused workspace regression tests")
	wv.AppendAgentOutput("reviewer", "[BLOCKED] waiting on a failing test")
	wv.updatePhases()

	plain := stripANSI(wv.Render(DefaultTheme))
	for _, want := range []string{
		"[workspace:01VISUAL]",
		"ARCHITECT",
		"▸ IMPLEMENTER",
		"REVIEWER",
		"codex",
		"claude",
		"working",
		"STUCK",
		"PR#17",
		"CI:running",
		"Running focused workspace regres...",
		"[working → implementer]",
		"✓",
		"⟳",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("workspace visual language missing %q:\n%s", want, plain)
		}
	}
	if got := wv.panes["reviewer"].Priority; got != workspace.AttentionRed {
		t.Fatalf("blocked reviewer priority = %d, want AttentionRed", got)
	}
}

func TestWorkspaceView_HeaderStaysOneRowWithLongPhases(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01LONGHEADER",
		Task:   strings.Repeat("wide workspace task ", 8),
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"architect-with-very-long-role": {
				Role:          "architect-with-very-long-role",
				Backend:       "claude",
				ActivityState: workspace.ActivityActive,
			},
			"implementer-with-very-long-role": {
				Role:          "implementer-with-very-long-role",
				Backend:       "codex",
				ActivityState: workspace.ActivityReady,
				PRID:          12,
				CIStatus:      workspace.CIPass,
			},
			"reviewer-with-very-long-role": {
				Role:          "reviewer-with-very-long-role",
				Backend:       "claude",
				ActivityState: workspace.ActivityBlocked,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(50, 18)
	wv.updatePhases()

	header := stripANSI(wv.renderHeader(DefaultTheme))
	if got := renderedLineCount(header); got != 1 {
		t.Fatalf("workspace header rendered %d rows, want 1:\n%s", got, header)
	}
	if got := lipgloss.Width(header); got > 50 {
		t.Fatalf("workspace header width = %d, want <= 50:\n%s", got, header)
	}
}

func TestWorkspaceView_EmptyAgents(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01EMPTY",
		Task:   "nothing",
		Status: workspace.WSSSpawning,
		Agents: map[string]*workspace.AgentRecord{},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 24)
	output := wv.Render(DefaultTheme)
	// Should not panic, should render something
	if output == "" {
		t.Fatal("Render returned empty for no agents")
	}
}

func TestWorkspaceView_AppendOutput_ThreadSafe(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01RACE",
		Task:   "race test",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"worker": {
				Role:          "worker",
				Backend:       "claude",
				ActivityState: workspace.ActivityActive,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 24)

	// Concurrent writes + reads
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			wv.AppendAgentOutput("worker", "line")
		}
		close(done)
	}()
	for i := 0; i < 100; i++ {
		wv.Render(DefaultTheme)
	}
	<-done
}

// stripANSI moved to internal/tui/ansi.go so production code (e.g.
// /share's markdown export) can use it without depending on a _test
// build tag.
