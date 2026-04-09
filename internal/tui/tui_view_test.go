package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// testApp creates a minimal App for view testing with a mock engine.
func testApp() *App {
	return New(nil, DefaultTheme, "test", "")
}

func TestTUIView_StartupRender(t *testing.T) {
	m := testApp()
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// Wait briefly then quit
	time.Sleep(500 * time.Millisecond)
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})

	out := readOutput(t, tm)

	// Verify key UI elements render
	if !strings.Contains(out, "altcode") {
		t.Error("missing 'altcode' header in startup view")
	}
}

func TestTUIView_HelpCommand(t *testing.T) {
	// Test /help text content directly (not via teatest — viewport scrolling
	// makes captured output incomplete). This verifies the help text function.
	helpText := builtinHelpText()
	checks := []string{
		"/workspace",
		"/workflow",
		"/rollback",
		"/send",
		"Workspace Mode",
		"Ctrl+Z",
		"Ctrl+Q",
	}
	for _, check := range checks {
		if !strings.Contains(helpText, check) {
			t.Errorf("missing %q in /help output", check)
		}
	}
}

func TestTUIView_WorkspaceViewRender(t *testing.T) {
	// Test the WorkspaceView renders correctly as a standalone component
	sess := &workspace.WorkspaceSession{
		ID:     "01TEST",
		Task:   "add JWT authentication",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"architect": {
				Role:          "architect",
				Backend:       "claude",
				ActivityState: workspace.ActivityActive,
				TurnCount:     3,
				CostUSD:       0.15,
				Branch:        "altcode/01TEST/architect/add-jwt",
			},
			"implementer": {
				Role:          "implementer",
				Backend:       "codex",
				ActivityState: workspace.ActivityExited,
				PRID:          42,
				CIStatus:      workspace.CIPass,
				TurnCount:     7,
				CostUSD:       0.32,
				Branch:        "altcode/01TEST/implementer/add-jwt",
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(120, 25)
	wv.AppendAgentOutput("architect", "Reading auth middleware...")
	wv.AppendAgentOutput("architect", "Designing JWT token flow...")
	wv.AppendAgentOutput("implementer", "Writing /api/login endpoint")
	wv.AppendAgentOutput("implementer", "Running tests... ok")

	output := wv.Render(DefaultTheme)

	// Verify workspace header
	if !strings.Contains(output, "workspace") {
		t.Error("missing workspace header")
	}

	// Verify agent panes render with role names
	plain := stripANSI(output)
	if !strings.Contains(plain, "ARCHITECT") {
		t.Error("missing ARCHITECT pane")
	}
	if !strings.Contains(plain, "IMPLEMENTER") {
		t.Error("missing IMPLEMENTER pane")
	}

	// Verify agent output appears
	if !strings.Contains(plain, "Reading auth middleware") {
		t.Error("missing architect output in pane")
	}
	if !strings.Contains(plain, "Running tests") {
		t.Error("missing implementer output in pane")
	}

	// Verify branch name shown
	if !strings.Contains(plain, "altcode/01TEST") {
		t.Error("missing branch name in pane")
	}

	// Verify CI status shown
	if !strings.Contains(plain, "CI:pass") {
		t.Error("missing CI status in pane")
	}

	// Verify PR shown
	if !strings.Contains(plain, "PR#42") {
		t.Error("missing PR number in pane")
	}

	// Verify footer
	if !strings.Contains(plain, "Ctrl+Z") {
		t.Error("missing Ctrl+Z in footer")
	}
}

func TestTUIView_WorkspaceViewNarrow(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01NARROW",
		Task:   "fix bug",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"fixer": {
				Role:          "fixer",
				Backend:       "codex",
				ActivityState: workspace.ActivityActive,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(60, 15)
	output := wv.Render(DefaultTheme)

	// Should render without panic
	if output == "" {
		t.Error("empty render on narrow terminal")
	}

	// No line should be excessively wide
	for i, line := range strings.Split(output, "\n") {
		plain := stripANSI(line)
		if len([]rune(plain)) > 65 { // 60 + some tolerance
			t.Errorf("line %d too wide: %d runes", i, len([]rune(plain)))
		}
	}
}

func TestTUIView_WorkspacePhases(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01PHASE",
		Task:   "test phases",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"architect": {
				Role:          "architect",
				Backend:       "claude",
				ActivityState: workspace.ActivityExited,
			},
			"implementer": {
				Role:          "implementer",
				Backend:       "codex",
				ActivityState: workspace.ActivityActive,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(120, 25)
	wv.updatePhases()

	output := wv.Render(DefaultTheme)
	plain := stripANSI(output)

	// Phase breadcrumb should show architect done, implementer running
	if !strings.Contains(plain, "✓") {
		t.Error("missing ✓ icon for completed architect phase")
	}
	if !strings.Contains(plain, "⟳") {
		t.Error("missing ⟳ icon for running implementer phase")
	}
}

func TestTUIView_AgentPaneAttentionColors(t *testing.T) {
	// Test that different priorities produce different renders
	paneGreen := &wsAgentPane{
		Role:     "worker",
		Backend:  "codex",
		Activity: workspace.ActivityActive,
		Priority: workspace.AttentionGreen,
	}
	paneRed := &wsAgentPane{
		Role:     "stuck",
		Backend:  "codex",
		Activity: workspace.ActivityBlocked,
		Priority: workspace.AttentionRed,
	}

	outGreen := paneGreen.Render(DefaultTheme, 50, 10)
	outRed := paneRed.Render(DefaultTheme, 50, 10)

	// Both should render (no panic)
	if outGreen == "" || outRed == "" {
		t.Error("empty pane render")
	}

	// They should be different (different colors in ANSI output)
	if outGreen == outRed {
		t.Error("green and red panes render identically")
	}
}

func TestTUIView_EmptyWorkspace(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01EMPTY",
		Task:   "nothing",
		Status: workspace.WSSSpawning,
		Agents: map[string]*workspace.AgentRecord{},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 24)
	output := wv.Render(DefaultTheme)

	if output == "" {
		t.Fatal("empty render for no agents")
	}
	if !strings.Contains(stripANSI(output), "No agents") {
		t.Error("should show 'No agents' message")
	}
}

func TestTUIView_ConcurrentRenderAndUpdate(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01RACE",
		Task:   "race test",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"worker": {
				Role:          "worker",
				Backend:       "codex",
				ActivityState: workspace.ActivityActive,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 24)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			wv.AppendAgentOutput("worker", "line")
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		wv.Render(DefaultTheme)
	}
	<-done
}

// readOutput reads all output from a test model after it quits.
func readOutput(t *testing.T, tm *teatest.TestModel) string {
	t.Helper()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	out, err := io.ReadAll(tm.FinalOutput(t))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return string(out)
}
