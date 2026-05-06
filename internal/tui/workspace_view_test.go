package tui

import (
	"strings"
	"testing"

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

// stripANSI removes ANSI escape sequences for width checking.
func stripANSI(s string) string {
	var result []rune
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		result = append(result, r)
	}
	return string(result)
}
