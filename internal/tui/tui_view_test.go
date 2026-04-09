package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
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

// --- Multi-Agent E2E View Tests ---

// TestTUIView_MultiAgent_2Codex2Claude tests workspace with 4 agents:
// 2 codex (architect, implementer) + 2 claude (reviewer, security).
func TestTUIView_MultiAgent_2Codex2Claude(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:         "01MULTI4",
		Task:       "build JWT authentication system",
		Status:     workspace.WSSWorking,
		BaseBranch: "main",
		Agents: map[string]*workspace.AgentRecord{
			"architect": {
				Role:          "architect",
				Backend:       "claude",
				Model:         "claude-sonnet-4-20250514",
				ActivityState: workspace.ActivityActive,
				TurnCount:     5,
				CostUSD:       0.23,
				Branch:        "altcode/01MULTI4/architect/build-jwt-auth",
			},
			"implementer": {
				Role:          "implementer",
				Backend:       "codex",
				Model:         "gpt-5.4",
				ActivityState: workspace.ActivityActive,
				TurnCount:     12,
				CostUSD:       0.45,
				Branch:        "altcode/01MULTI4/implementer/build-jwt-auth",
				PRID:          101,
				CIStatus:      workspace.CIRunning,
			},
			"reviewer": {
				Role:          "reviewer",
				Backend:       "claude",
				Model:         "claude-sonnet-4-20250514",
				ActivityState: workspace.ActivityExited,
				TurnCount:     3,
				CostUSD:       0.11,
				Branch:        "altcode/01MULTI4/reviewer/build-jwt-auth",
				PRID:          102,
				CIStatus:      workspace.CIPass,
				ReviewStatus:  workspace.ReviewApproved,
			},
			"security": {
				Role:          "security",
				Backend:       "codex",
				Model:         "gpt-5.4",
				ActivityState: workspace.ActivityBlocked,
				TurnCount:     2,
				CostUSD:       0.08,
				Branch:        "altcode/01MULTI4/security/build-jwt-auth",
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(160, 40)

	// Stream output from each agent
	wv.AppendAgentOutput("architect", "Designing JWT token flow with RS256 signing...")
	wv.AppendAgentOutput("architect", "Created middleware spec in docs/auth-design.md")
	wv.AppendAgentOutput("implementer", "Writing /api/login endpoint...")
	wv.AppendAgentOutput("implementer", "Writing /api/refresh endpoint...")
	wv.AppendAgentOutput("implementer", "Running tests... 15 passed, 0 failed")
	wv.AppendAgentOutput("reviewer", "Reviewed: LGTM, approved PR #102")
	wv.AppendAgentOutput("security", "[BLOCKED] Waiting for implementer to finish")

	wv.updatePhases()
	output := wv.Render(DefaultTheme)
	plain := stripANSI(output)

	// All 4 agent panes must render
	for _, role := range []string{"ARCHITECT", "IMPLEMENTER", "REVIEWER", "SECURITY"} {
		if !strings.Contains(plain, role) {
			t.Errorf("missing %s pane in 4-agent workspace", role)
		}
	}

	// Both backends visible
	if !strings.Contains(plain, "claude") {
		t.Error("missing claude backend label")
	}
	if !strings.Contains(plain, "codex") {
		t.Error("missing codex backend label")
	}

	// Agent output visible
	if !strings.Contains(plain, "JWT token flow") {
		t.Error("missing architect output")
	}
	if !strings.Contains(plain, "15 passed") {
		t.Error("missing implementer test output")
	}

	// PR and CI info
	if !strings.Contains(plain, "PR#101") {
		t.Error("missing implementer PR#101")
	}
	if !strings.Contains(plain, "PR#102") {
		t.Error("missing reviewer PR#102")
	}

	// Phase breadcrumb: reviewer done (✓), architect+implementer running (⟳), security blocked (✗)
	if !strings.Contains(plain, "✓") {
		t.Error("missing ✓ for completed reviewer phase")
	}
	if !strings.Contains(plain, "⟳") {
		t.Error("missing ⟳ for active agent phases")
	}
	if !strings.Contains(plain, "✗") {
		t.Error("missing ✗ for blocked security phase")
	}

	// Blocked agent should have different attention color
	secPane := wv.panes["security"]
	if secPane.Priority != workspace.AttentionRed {
		t.Errorf("blocked agent should be AttentionRed, got %d", secPane.Priority)
	}

	// Focus cycling works across all 4 panes
	wv.CycleFocus() // 0 -> architect
	wv.CycleFocus() // 1 -> implementer
	wv.CycleFocus() // 2 -> reviewer
	wv.CycleFocus() // 3 -> security
	if wv.FocusedRole() != wv.order[3] {
		t.Errorf("expected focus on 4th role, got %q", wv.FocusedRole())
	}
	wv.CycleFocus() // wraps to 0
	if wv.FocusedRole() != wv.order[0] {
		t.Errorf("focus should wrap to first role, got %q", wv.FocusedRole())
	}
}

// TestTUIView_MultiAgent_GitBranches tests that each agent's git branch
// is correctly displayed in the pane (context/git management).
func TestTUIView_MultiAgent_GitBranches(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:         "01GITBR",
		Task:       "implement search feature",
		Status:     workspace.WSSWorking,
		BaseBranch: "main",
		Agents: map[string]*workspace.AgentRecord{
			"designer": {
				Role:          "designer",
				Backend:       "claude",
				ActivityState: workspace.ActivityActive,
				Branch:        "altcode/01GITBR/designer/implement-search-feature",
			},
			"coder": {
				Role:          "coder",
				Backend:       "codex",
				ActivityState: workspace.ActivityActive,
				Branch:        "altcode/01GITBR/coder/implement-search-feature",
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(120, 25)
	output := wv.Render(DefaultTheme)
	plain := stripANSI(output)

	// Both branches should be visible (with ⎇ prefix)
	if !strings.Contains(plain, "altcode/01GITBR/designer") {
		t.Error("missing designer branch in pane")
	}
	if !strings.Contains(plain, "altcode/01GITBR/coder") {
		t.Error("missing coder branch in pane")
	}
}

// TestTUIView_LifecycleTransitions tests workspace status transitions
// are reflected in the header.
func TestTUIView_LifecycleTransitions(t *testing.T) {
	transitions := []struct {
		status workspace.WorkspaceStatus
		want   string
	}{
		{workspace.WSSSpawning, "spawning"},
		{workspace.WSSWorking, "working"},
		{workspace.WSSPROpen, "pr_open"},
		{workspace.WSSCIChecking, "ci_checking"},
		{workspace.WSSCIFailed, "ci_failed"},
		{workspace.WSSApproved, "approved"},
		{workspace.WSSDone, "done"},
		{workspace.WSSFailed, "failed"},
		{workspace.WSSPaused, "paused"},
	}

	for _, tc := range transitions {
		t.Run(string(tc.status), func(t *testing.T) {
			sess := &workspace.WorkspaceSession{
				ID:     "01LIFE",
				Task:   "test lifecycle",
				Status: tc.status,
				Agents: map[string]*workspace.AgentRecord{
					"worker": {
						Role:          "worker",
						Backend:       "codex",
						ActivityState: workspace.ActivityActive,
					},
				},
			}
			wv := NewWorkspaceView(sess)
			wv.SetSize(120, 20)
			output := wv.Render(DefaultTheme)
			plain := stripANSI(output)
			if !strings.Contains(plain, tc.want) {
				t.Errorf("workspace status %q not visible in render", tc.want)
			}
		})
	}
}

// TestTUIView_CIStatusPerAgent tests CI status rendering for each state.
func TestTUIView_CIStatusPerAgent(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01CIST",
		Task:   "ci status test",
		Status: workspace.WSSCIChecking,
		Agents: map[string]*workspace.AgentRecord{
			"pass-agent": {
				Role:          "pass-agent",
				Backend:       "codex",
				ActivityState: workspace.ActivityExited,
				PRID:          10,
				CIStatus:      workspace.CIPass,
			},
			"fail-agent": {
				Role:          "fail-agent",
				Backend:       "claude",
				ActivityState: workspace.ActivityActive,
				PRID:          11,
				CIStatus:      workspace.CIFail,
			},
			"pending-agent": {
				Role:          "pending-agent",
				Backend:       "codex",
				ActivityState: workspace.ActivityActive,
				PRID:          12,
				CIStatus:      workspace.CIPending,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(160, 30)
	output := wv.Render(DefaultTheme)
	plain := stripANSI(output)

	if !strings.Contains(plain, "CI:pass") {
		t.Error("missing CI:pass for pass-agent")
	}
	if !strings.Contains(plain, "CI:fail") {
		t.Error("missing CI:fail for fail-agent")
	}
	if !strings.Contains(plain, "CI:pending") {
		t.Error("missing CI:pending for pending-agent")
	}

	// CI fail agent should be AttentionYellow
	failPane := wv.panes["fail-agent"]
	if failPane.Priority != workspace.AttentionYellow {
		t.Errorf("CI-fail agent should be AttentionYellow, got %d", failPane.Priority)
	}
}

// TestTUIView_UpdateAgent_DynamicStateChange tests that UpdateAgent
// correctly refreshes pane state (simulating lifecycle polling).
func TestTUIView_UpdateAgent_DynamicStateChange(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01DYN",
		Task:   "dynamic update test",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"worker": {
				Role:          "worker",
				Backend:       "codex",
				ActivityState: workspace.ActivitySpawning,
				CIStatus:      workspace.CIUnknown,
			},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 20)

	// Initially spawning → "starting…"
	out1 := stripANSI(wv.Render(DefaultTheme))
	if !strings.Contains(out1, "starting") {
		t.Errorf("should show starting state initially, got:\n%s", out1)
	}

	// Transition to active → "working"
	rec := sess.Agents["worker"]
	rec.ActivityState = workspace.ActivityActive
	rec.TurnCount = 5
	rec.CostUSD = 0.15
	wv.UpdateAgent(rec)

	out2 := stripANSI(wv.Render(DefaultTheme))
	if !strings.Contains(out2, "working") {
		t.Errorf("should show working state after update, got:\n%s", out2)
	}
	if !strings.Contains(out2, "turns:5") {
		t.Error("should show updated turn count")
	}
	if !strings.Contains(out2, "$0.15") {
		t.Error("should show updated cost")
	}

	// Transition to exited with PR
	rec.ActivityState = workspace.ActivityExited
	rec.PRID = 99
	rec.CIStatus = workspace.CIPass
	wv.UpdateAgent(rec)

	out3 := stripANSI(wv.Render(DefaultTheme))
	if !strings.Contains(out3, "done") {
		t.Errorf("should show done state after update, got:\n%s", out3)
	}
	if !strings.Contains(out3, "PR#99") {
		t.Error("should show PR number after update")
	}
}

// TestTUIView_HasRole_And_FocusedRole tests role presence checking
// (used by /send command validation in commands.go).
func TestTUIView_HasRole_And_FocusedRole(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01ROLE",
		Task:   "role test",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"alpha": {Role: "alpha", Backend: "claude", ActivityState: workspace.ActivityActive},
			"beta":  {Role: "beta", Backend: "codex", ActivityState: workspace.ActivityActive},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 20)

	if !wv.HasRole("alpha") {
		t.Error("HasRole should return true for 'alpha'")
	}
	if !wv.HasRole("beta") {
		t.Error("HasRole should return true for 'beta'")
	}
	if wv.HasRole("gamma") {
		t.Error("HasRole should return false for 'gamma'")
	}

	// No focus initially
	if wv.FocusedRole() != "" {
		t.Error("should have no focus initially")
	}

	wv.CycleFocus()
	focused := wv.FocusedRole()
	if focused != wv.order[0] {
		t.Errorf("first focus should be %q, got %q", wv.order[0], focused)
	}
}

// TestTUIView_PauseState tests that paused state renders in footer.
func TestTUIView_PauseState(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01PAUSE",
		Task:   "pause test",
		Status: workspace.WSSPaused,
		Agents: map[string]*workspace.AgentRecord{
			"worker": {Role: "worker", Backend: "codex", ActivityState: workspace.ActivityActive},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 20)
	wv.SetPaused(true)

	output := stripANSI(wv.Render(DefaultTheme))
	if !strings.Contains(output, "PAUSED") {
		t.Error("should show PAUSED in footer when paused")
	}
}

// TestTUIView_OutputScrollback tests rolling buffer behavior.
func TestTUIView_OutputScrollback(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01SCROLL",
		Task:   "scroll test",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"streamer": {Role: "streamer", Backend: "codex", ActivityState: workspace.ActivityActive},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 20)

	// Push more lines than the rolling buffer (200)
	for i := 0; i < 300; i++ {
		wv.AppendAgentOutput("streamer", fmt.Sprintf("line-%03d", i))
	}

	// The pane should keep only the last 200 lines
	pane := wv.panes["streamer"]
	if len(pane.Lines) != agentPaneOutputLines {
		t.Errorf("expected %d lines in buffer, got %d", agentPaneOutputLines, len(pane.Lines))
	}

	// Earliest line should be line-100 (after discarding 0-99)
	if pane.Lines[0] != "line-100" {
		t.Errorf("expected first buffered line 'line-100', got %q", pane.Lines[0])
	}
}

// TestTUIView_ConcurrentMultiAgent tests race safety with 4 agents
// writing concurrently while render is happening.
func TestTUIView_ConcurrentMultiAgent(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01RACE4",
		Task:   "concurrent 4-agent test",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"a": {Role: "a", Backend: "claude", ActivityState: workspace.ActivityActive},
			"b": {Role: "b", Backend: "codex", ActivityState: workspace.ActivityActive},
			"c": {Role: "c", Backend: "claude", ActivityState: workspace.ActivityActive},
			"d": {Role: "d", Backend: "codex", ActivityState: workspace.ActivityActive},
		},
	}

	wv := NewWorkspaceView(sess)
	wv.SetSize(160, 40)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			wv.AppendAgentOutput("a", "claude-output")
			wv.AppendAgentOutput("b", "codex-output")
			wv.AppendAgentOutput("c", "claude-output-2")
			wv.AppendAgentOutput("d", "codex-output-2")
		}
	}()

	for i := 0; i < 200; i++ {
		wv.Render(DefaultTheme)
		wv.updatePhases()
	}
	<-done
}

// --- Tool Tree Collapse Tests ---

func TestToolTree_CollapseConsecutiveReads(t *testing.T) {
	tree := newToolTree()

	// Simulate 5 consecutive Read calls
	for i := 0; i < 5; i++ {
		tree.Start("Read", fmt.Sprintf("file%d.go", i))
		tree.Done(fmt.Sprintf("file%d.go", i), 10*time.Millisecond)
	}
	// Then a different tool
	tree.Start("Edit", "main.go")
	tree.Done("main.go", 50*time.Millisecond)

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	// 5 consecutive Reads should be collapsed into one summary
	if !strings.Contains(plain, "Read 5 files") {
		t.Errorf("expected 'Read 5 files' collapse, got:\n%s", plain)
	}
	// Edit should still show individually
	if !strings.Contains(plain, "Edit") {
		t.Error("missing individual Edit entry")
	}
}

func TestToolTree_NoCollapseUnder3(t *testing.T) {
	tree := newToolTree()

	// 2 consecutive Reads — should NOT collapse
	tree.Start("Read", "a.go")
	tree.Done("a.go", 5*time.Millisecond)
	tree.Start("Read", "b.go")
	tree.Done("b.go", 5*time.Millisecond)

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	// Should show individual entries, not collapsed
	if strings.Contains(plain, "Read 2 files") {
		t.Error("should not collapse 2 entries")
	}
	if !strings.Contains(plain, "a.go") {
		t.Error("missing individual entry a.go")
	}
}

func TestToolTree_MixedToolsCollapse(t *testing.T) {
	tree := newToolTree()

	// 4 Grep, then 3 Read, then 1 Bash
	for i := 0; i < 4; i++ {
		tree.Start("Grep", fmt.Sprintf("pattern%d", i))
		tree.Done(fmt.Sprintf("pattern%d", i), 8*time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		tree.Start("Read", fmt.Sprintf("file%d.go", i))
		tree.Done(fmt.Sprintf("file%d.go", i), 5*time.Millisecond)
	}
	tree.Start("Bash", "go test ./...")
	tree.Done("go test ./...", 1000*time.Millisecond)

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	if !strings.Contains(plain, "Searched 4 patterns") {
		t.Errorf("expected 'Searched 4 patterns', got:\n%s", plain)
	}
	if !strings.Contains(plain, "Read 3 files") {
		t.Errorf("expected 'Read 3 files', got:\n%s", plain)
	}
	if !strings.Contains(plain, "Bash") {
		t.Error("missing individual Bash entry")
	}
}

func TestToolTree_RunningNotCollapsed(t *testing.T) {
	tree := newToolTree()

	// 3 completed Reads + 1 running Read — running should not be collapsed
	for i := 0; i < 3; i++ {
		tree.Start("Read", fmt.Sprintf("done%d.go", i))
		tree.Done(fmt.Sprintf("done%d.go", i), 5*time.Millisecond)
	}
	tree.Start("Read", "active.go")
	// Don't call Done — this one is still running

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	// The 3 done Reads should collapse
	if !strings.Contains(plain, "Read 3 files") {
		t.Errorf("expected 'Read 3 files' collapse, got:\n%s", plain)
	}
	// The running one should show individually with ⟳
	if !strings.Contains(plain, "⟳") {
		t.Error("missing running indicator for active Read")
	}
}

// === CC Feature Parity Verification Tests ===
// Each test verifies one CC TUI feature is actually working in altcode.

func TestCCParity_HUD_GitDirtyIndicator(t *testing.T) {
	hs := hudState{
		GitProject: "altcode",
		GitBranch:  "main",
		GitDirty:   true,
	}
	info := statusBarInfo{Model: "opus-4-6"}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	if !strings.Contains(plain, "main*") {
		t.Errorf("git dirty indicator missing — expected 'main*', got:\n%s", plain)
	}
	if !strings.Contains(plain, "altcode") {
		t.Error("project name missing from HUD")
	}
}

func TestCCParity_HUD_GitClean(t *testing.T) {
	hs := hudState{
		GitProject: "myproject",
		GitBranch:  "feature",
		GitDirty:   false,
	}
	info := statusBarInfo{Model: "sonnet"}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	// Should NOT have * when clean
	if strings.Contains(plain, "feature*") {
		t.Error("dirty indicator shown when git is clean")
	}
	if !strings.Contains(plain, "feature") {
		t.Error("branch name missing")
	}
}

func TestCCParity_HUD_ConfigCounts(t *testing.T) {
	hs := hudState{
		ClaudeMDCount: 2,
		MCPCount:      4,
		HooksCount:    3,
		ContextLimit:  128000,
		ContextTokens: 50000,
	}
	info := statusBarInfo{Model: "opus"}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	if !strings.Contains(plain, "2 CLAUDE.md") {
		t.Errorf("missing config count '2 CLAUDE.md' in:\n%s", plain)
	}
	if !strings.Contains(plain, "4 MCPs") {
		t.Errorf("missing config count '4 MCPs' in:\n%s", plain)
	}
	if !strings.Contains(plain, "3 hooks") {
		t.Errorf("missing config count '3 hooks' in:\n%s", plain)
	}
}

func TestCCParity_HUD_TaskProgress(t *testing.T) {
	hs := hudState{
		TasksTotal:     5,
		TasksDone:      3,
		ActiveTaskName: "Implementing auth middleware",
		ContextLimit:   128000,
	}
	info := statusBarInfo{Model: "opus"}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	// Should show ▸ active task (3/5)
	if !strings.Contains(plain, "▸") {
		t.Errorf("missing ▸ active task indicator in:\n%s", plain)
	}
	if !strings.Contains(plain, "Implementing auth middleware") {
		t.Errorf("missing active task name in:\n%s", plain)
	}
	if !strings.Contains(plain, "3/5") {
		t.Errorf("missing task progress counter in:\n%s", plain)
	}
}

func TestCCParity_HUD_AllTasksComplete(t *testing.T) {
	hs := hudState{
		TasksTotal: 5,
		TasksDone:  5,
		ContextLimit: 128000,
	}
	info := statusBarInfo{Model: "opus"}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	if !strings.Contains(plain, "All tasks") {
		t.Errorf("missing 'All tasks complete' when all done:\n%s", plain)
	}
	if !strings.Contains(plain, "5/5") {
		t.Errorf("missing final count 5/5 in:\n%s", plain)
	}
}

func TestCCParity_HUD_ContextBar(t *testing.T) {
	hs := hudState{
		ContextTokens: 64000,
		ContextLimit:  128000, // 50%
		SessionStart:  time.Now().Add(-10 * time.Minute),
	}
	info := statusBarInfo{Model: "opus", CostUSD: 0.1234}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	if !strings.Contains(plain, "50%") {
		t.Errorf("missing context percentage in:\n%s", plain)
	}
	if !strings.Contains(plain, "█") {
		t.Error("missing filled blocks in context bar")
	}
	if !strings.Contains(plain, "░") {
		t.Error("missing empty blocks in context bar")
	}
	if !strings.Contains(plain, "$0.1234") {
		t.Errorf("missing cost display in:\n%s", plain)
	}
}

func TestCCParity_HUD_RunningToolWithTarget(t *testing.T) {
	hs := hudState{ContextLimit: 128000}
	info := statusBarInfo{
		Model:      "opus",
		ToolActive: "Bash (go test ./...)",
	}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "⠋")
	plain := stripANSI(output)

	if !strings.Contains(plain, "Bash") {
		t.Errorf("missing running tool name in:\n%s", plain)
	}
	if !strings.Contains(plain, "go test") {
		t.Errorf("missing tool target in:\n%s", plain)
	}
}

func TestCCParity_ExtractToolTarget(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    map[string]any
		want     string
	}{
		{
			name:     "Read file_path",
			toolName: "Read",
			input:    map[string]any{"file_path": "/home/user/project/src/main.go"},
			want:     "main.go",
		},
		{
			name:     "Edit file_path",
			toolName: "Edit",
			input:    map[string]any{"file_path": "/home/user/project/internal/tui/app.go"},
			want:     "app.go",
		},
		{
			name:     "Grep pattern",
			toolName: "Grep",
			input:    map[string]any{"pattern": "func.*Update"},
			want:     "func.*Update",
		},
		{
			name:     "Bash command truncated",
			toolName: "Bash",
			input:    map[string]any{"command": "GOFLAGS=-mod=mod go test ./... -race -count=1 -timeout=180s"},
			want:     "GOFLAGS=-mod=mod go test ./.",
		},
		{
			name:     "Empty input",
			toolName: "Read",
			input:    map[string]any{},
			want:     "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inputJSON, _ := json.Marshal(tc.input)
			toolCall := &event.ToolCall{
				Name:  tc.toolName,
				Input: json.RawMessage(inputJSON),
			}
			got := extractToolTarget(toolCall)
			if !strings.Contains(got, tc.want) && tc.want != "" {
				t.Errorf("extractToolTarget(%s) = %q, want containing %q", tc.toolName, got, tc.want)
			}
			if tc.want == "" && got != "" {
				t.Errorf("extractToolTarget(%s) = %q, want empty", tc.toolName, got)
			}
		})
	}
}

func TestCCParity_TrackTaskFromTool(t *testing.T) {
	app := testApp()

	// TaskCreate
	input1, _ := json.Marshal(map[string]any{
		"subject":    "Fix authentication bug",
		"activeForm": "Fixing authentication bug",
	})
	app.trackTaskFromTool(&event.ToolCall{
		Name:  "TaskCreate",
		Input: json.RawMessage(input1),
	})
	if app.tasksTotal != 1 {
		t.Errorf("tasksTotal = %d, want 1", app.tasksTotal)
	}
	if app.activeTaskName != "Fixing authentication bug" {
		t.Errorf("activeTaskName = %q, want 'Fixing authentication bug'", app.activeTaskName)
	}

	// TaskCreate another
	input2, _ := json.Marshal(map[string]any{
		"subject": "Write tests",
	})
	app.trackTaskFromTool(&event.ToolCall{
		Name:  "TaskCreate",
		Input: json.RawMessage(input2),
	})
	if app.tasksTotal != 2 {
		t.Errorf("tasksTotal = %d, want 2", app.tasksTotal)
	}

	// TaskUpdate to completed
	input3, _ := json.Marshal(map[string]any{
		"status": "completed",
	})
	app.trackTaskFromTool(&event.ToolCall{
		Name:  "TaskUpdate",
		Input: json.RawMessage(input3),
	})
	if app.tasksDone != 1 {
		t.Errorf("tasksDone = %d, want 1", app.tasksDone)
	}
	if app.activeTaskName != "" {
		t.Errorf("activeTaskName should be empty after completion, got %q", app.activeTaskName)
	}

	// TaskUpdate to in_progress with activeForm
	input4, _ := json.Marshal(map[string]any{
		"status":     "in_progress",
		"activeForm": "Running test suite",
	})
	app.trackTaskFromTool(&event.ToolCall{
		Name:  "TaskUpdate",
		Input: json.RawMessage(input4),
	})
	if app.activeTaskName != "Running test suite" {
		t.Errorf("activeTaskName = %q, want 'Running test suite'", app.activeTaskName)
	}
}

func TestCCParity_ToolTree_WithTargets(t *testing.T) {
	tree := newToolTree()

	tree.Start("Read", "main.go")
	tree.Done("main.go", 15*time.Millisecond)
	tree.Start("Bash", "go test ./...")
	tree.Done("go test ./...", 2500*time.Millisecond)
	tree.Start("Edit", "app.go")
	// Still running

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	if !strings.Contains(plain, "Read") {
		t.Error("missing Read entry")
	}
	if !strings.Contains(plain, "main.go") {
		t.Error("missing Read target 'main.go'")
	}
	if !strings.Contains(plain, "<1s") {
		t.Error("missing Read timing (<1s)")
	}
	if !strings.Contains(plain, "Bash") {
		t.Error("missing Bash entry")
	}
	if !strings.Contains(plain, "go test") {
		t.Error("missing Bash target")
	}
	if !strings.Contains(plain, "⟳") {
		t.Error("missing running indicator for Edit")
	}
	if !strings.Contains(plain, "✓") {
		t.Error("missing done indicator")
	}
}

func TestCCParity_HUD_ToolCountsDisplay(t *testing.T) {
	hs := hudState{
		ContextLimit: 128000,
		ToolCounts:   map[string]int{"Read": 12, "Edit": 3, "Bash": 8, "Grep": 5},
	}
	info := statusBarInfo{Model: "opus"}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	// Should show tool counts like CC: ✓ Read ×12
	if !strings.Contains(plain, "×") {
		t.Errorf("missing tool count indicators in:\n%s", plain)
	}
}

func TestCCParity_StuckToolMsg_Handling(t *testing.T) {
	startTime := time.Now().Add(-35 * time.Second)

	app := testApp()
	app.activeToolName = "Bash"
	app.toolStart = startTime
	app.busy = true

	// Matching name + startedAt should set detail
	msg1 := stuckToolMsg{name: "Bash", startedAt: startTime}
	app.Update(msg1)
	if app.activeToolDetail == "" {
		t.Error("stuckToolMsg should set activeToolDetail for matching tool")
	}

	// Reset and test name mismatch (same startedAt to isolate name check)
	app.activeToolDetail = ""
	app.activeToolName = "Bash"
	app.toolStart = startTime
	msg2 := stuckToolMsg{name: "Read", startedAt: startTime}
	app.Update(msg2)
	if app.activeToolDetail != "" {
		t.Error("stuckToolMsg should NOT set detail when name doesn't match")
	}

	// Test startedAt mismatch (same name to isolate time check)
	app.activeToolDetail = ""
	app.activeToolName = "Bash"
	app.toolStart = startTime
	msg3 := stuckToolMsg{name: "Bash", startedAt: startTime.Add(-10 * time.Second)}
	app.Update(msg3)
	if app.activeToolDetail != "" {
		t.Error("stuckToolMsg should NOT set detail when startedAt doesn't match")
	}
}

func TestCCParity_HUD_SessionDuration(t *testing.T) {
	hs := hudState{
		SessionStart: time.Now().Add(-65 * time.Minute),
		ContextLimit: 128000,
	}
	info := statusBarInfo{Model: "opus"}
	output := renderHUD(hs, info, DefaultTheme, 120, false, "")
	plain := stripANSI(output)

	// Should show duration like "1h5m"
	if !strings.Contains(plain, "1h") {
		t.Errorf("missing session duration in:\n%s", plain)
	}
}

func TestCCParity_FullHUD_AllFeatures(t *testing.T) {
	// Simulate a realistic CC-like HUD with ALL features populated
	hs := hudState{
		ContextTokens:  480000,
		ContextLimit:   1000000,
		SessionStart:   time.Now().Add(-2 * time.Hour),
		GitProject:     "altcode",
		GitBranch:      "main",
		GitDirty:       true,
		ClaudeMDCount:  2,
		MCPCount:       4,
		HooksCount:     3,
		ToolCounts:     map[string]int{"Bash": 8, "Read": 12, "Edit": 4, "Grep": 3},
		TasksTotal:     5,
		TasksDone:      3,
		ActiveTaskName: "Writing E2E tests",
	}
	info := statusBarInfo{
		Model:      "anthropic/claude-opus-4-6",
		TokensIn:   480000,
		TokensOut:  40000,
		CostUSD:    1.2345,
		ToolActive: "",
	}
	output := renderHUD(hs, info, DefaultTheme, 160, false, "")
	plain := stripANSI(output)

	checks := map[string]string{
		"model":       "claude-opus-4-6",
		"git project": "altcode",
		"git branch":  "main*",
		"context %":   "48%",
		"context bar": "█",
		"cost":        "$1.2345",
		"CLAUDE.md":   "2 CLAUDE.md",
		"MCPs":        "4 MCPs",
		"hooks":       "3 hooks",
		"task active": "▸",
		"task name":   "Writing E2E tests",
		"task count":  "3/5",
		"duration":    "2h",
	}
	for label, want := range checks {
		if !strings.Contains(plain, want) {
			t.Errorf("FULL HUD missing %s (%q) in:\n%s", label, want, plain)
		}
	}
}

// === Tool Output Display Tests (CC style) ===

func TestCCStyle_EditDiffOutput(t *testing.T) {
	tree := newToolTree()
	tree.Start("Edit", "app.go")
	diffOutput := "- old line removed\n+ new line added\n  context line\n+ another addition"
	tree.DoneWithOutput("app.go", 50*time.Millisecond, diffOutput)

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	// Should show Edit(app.go) in CC style
	if !strings.Contains(plain, "Edit(app.go)") {
		t.Errorf("missing CC-style 'Edit(app.go)' in:\n%s", plain)
	}
	// Should show ⎿ connector for output lines
	if !strings.Contains(plain, "⎿") {
		t.Errorf("missing ⎿ output connector in:\n%s", plain)
	}
	// Should show diff content
	if !strings.Contains(plain, "old line removed") {
		t.Errorf("missing diff removed line in:\n%s", plain)
	}
	if !strings.Contains(plain, "new line added") {
		t.Errorf("missing diff added line in:\n%s", plain)
	}
}

func TestCCStyle_BashCommandOutput(t *testing.T) {
	tree := newToolTree()
	tree.Start("Bash", "go test ./... -race")
	bashOutput := "ok  github.com/altcode-ai/altcode/internal/tui  2.902s\nok  github.com/altcode-ai/altcode/cmd/altcode  1.2s"
	tree.DoneWithOutput("go test", 2500*time.Millisecond, bashOutput)

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	// Should show Bash(command) in CC style
	if !strings.Contains(plain, "Bash(go test") {
		t.Errorf("missing CC-style 'Bash(command)' in:\n%s", plain)
	}
	// Should show ⎿ with output
	if !strings.Contains(plain, "⎿") {
		t.Errorf("missing ⎿ output connector in:\n%s", plain)
	}
	// Should show test output
	if !strings.Contains(plain, "2.902s") {
		t.Errorf("missing bash output in:\n%s", plain)
	}
}

func TestCCStyle_OutputTruncation(t *testing.T) {
	tree := newToolTree()
	tree.Start("Bash", "long command")

	// Generate 20 lines of output — should be truncated to 8
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf("output line %d", i))
	}
	tree.DoneWithOutput("long command", 100*time.Millisecond, strings.Join(lines, "\n"))

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	// Should show truncation indicator (CC style: "… +N lines")
	if !strings.Contains(plain, "+12 lines") {
		t.Errorf("missing truncation indicator in:\n%s", plain)
	}
	// Should show first lines
	if !strings.Contains(plain, "output line 0") {
		t.Error("missing first output line")
	}
}

func TestCCStyle_NoOutputForRead(t *testing.T) {
	tree := newToolTree()
	tree.Start("Read", "file.go")
	tree.Done("file.go", 10*time.Millisecond)

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	// Read should NOT show output (no ⎿ lines)
	if strings.Contains(plain, "⎿") {
		t.Error("Read should not show output lines")
	}
	// Should show CC-style name
	if !strings.Contains(plain, "Read(file.go)") {
		t.Errorf("missing CC-style 'Read(file.go)' in:\n%s", plain)
	}
}

func TestCCStyle_NameParenFormat(t *testing.T) {
	tree := newToolTree()
	tree.Start("Grep", "func.*Update")
	tree.Done("func.*Update", 8*time.Millisecond)
	tree.Start("Glob", "**/*.go")
	tree.Done("**/*.go", 5*time.Millisecond)

	output := tree.Render(DefaultTheme, 120)
	plain := stripANSI(output)

	if !strings.Contains(plain, "Grep(func.*Update)") {
		t.Errorf("missing 'Grep(pattern)' in:\n%s", plain)
	}
	if !strings.Contains(plain, "Glob(**/*.go)") {
		t.Errorf("missing 'Glob(pattern)' in:\n%s", plain)
	}
}

// readOutput reads all output from a test model after it quits.
func TestCCStyle_ThinkingIndicator(t *testing.T) {
	app := testApp()
	app.thinking = true
	app.turnStart = time.Now().Add(-65 * time.Second)
	app.tokensOut = 1500
	app.theme = DefaultTheme

	output := app.renderThinkingIndicator()
	plain := stripANSI(output)

	// Should have the star icon
	if !strings.Contains(plain, "✶") {
		t.Errorf("missing ✶ icon in thinking indicator:\n%s", plain)
	}
	// Should have a verb
	hasVerb := false
	for _, v := range thinkingVerbs {
		if strings.Contains(plain, v) {
			hasVerb = true
			break
		}
	}
	if !hasVerb {
		t.Errorf("no thinking verb found in:\n%s", plain)
	}
	// Should show duration (1m 5s)
	if !strings.Contains(plain, "1m 5s") {
		t.Errorf("missing duration '1m 5s' in:\n%s", plain)
	}
	// Should show token count with ↓ (input/context direction during thinking)
	if !strings.Contains(plain, "↓") || !strings.Contains(plain, "tokens") {
		t.Errorf("missing token count with ↓ in:\n%s", plain)
	}
	// Should have tip line with ⎿
	if !strings.Contains(plain, "⎿") {
		t.Errorf("missing tip connector ⎿ in:\n%s", plain)
	}
	if !strings.Contains(plain, "Esc") {
		t.Errorf("missing Esc tip in:\n%s", plain)
	}
}

func TestCCStyle_ThinkingVerbRotates(t *testing.T) {
	app := testApp()
	app.thinking = true
	app.theme = DefaultTheme

	// At 0s: first verb
	app.turnStart = time.Now()
	out1 := stripANSI(app.renderThinkingIndicator())

	// At 3s: should rotate to next verb
	app.turnStart = time.Now().Add(-3 * time.Second)
	out2 := stripANSI(app.renderThinkingIndicator())

	// At 6s: should rotate again
	app.turnStart = time.Now().Add(-6 * time.Second)
	out3 := stripANSI(app.renderThinkingIndicator())

	// Not all three should be the same (rotation works)
	if out1 == out2 && out2 == out3 {
		t.Error("thinking verb should rotate over time")
	}
}

// === Focus + Scroll Behavior Tests ===

func TestWorkspace_ClickFocusHorizontal(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID: "01CLICK", Task: "click test", Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"alpha": {Role: "alpha", Backend: "claude", ActivityState: workspace.ActivityActive},
			"beta":  {Role: "beta", Backend: "codex", ActivityState: workspace.ActivityActive},
			"gamma": {Role: "gamma", Backend: "claude", ActivityState: workspace.ActivityActive},
		},
	}
	wv := NewWorkspaceView(sess)
	wv.SetSize(120, 30) // horizontal layout (3 agents, 120 cols)

	// Click in first pane area (x=10)
	wv.FocusByClick(10, 15)
	if wv.FocusedRole() != wv.order[0] {
		t.Errorf("click x=10 should focus first pane, got %q", wv.FocusedRole())
	}

	// Click in last pane area (x=100)
	wv.FocusByClick(100, 15)
	if wv.FocusedRole() != wv.order[2] {
		t.Errorf("click x=100 should focus third pane, got %q", wv.FocusedRole())
	}
}

func TestWorkspace_ClickFocusVertical(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID: "01VERT", Task: "vert test", Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"a": {Role: "a", Backend: "claude", ActivityState: workspace.ActivityActive},
			"b": {Role: "b", Backend: "codex", ActivityState: workspace.ActivityActive},
			"c": {Role: "c", Backend: "claude", ActivityState: workspace.ActivityActive},
			"d": {Role: "d", Backend: "codex", ActivityState: workspace.ActivityActive},
		},
	}
	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 40) // vertical layout (4 agents, <120 cols)

	// Click near top → first pane
	wv.FocusByClick(40, 3)
	if wv.FocusedRole() != wv.order[0] {
		t.Errorf("click y=3 should focus first pane, got %q", wv.FocusedRole())
	}

	// Click near bottom → last pane
	wv.FocusByClick(40, 35)
	if wv.FocusedRole() != wv.order[3] {
		t.Errorf("click y=35 should focus last pane, got %q", wv.FocusedRole())
	}
}

func TestWorkspace_ScrollPane(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID: "01SCROLL2", Task: "scroll", Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"worker": {Role: "worker", Backend: "codex", ActivityState: workspace.ActivityActive},
		},
	}
	wv := NewWorkspaceView(sess)
	wv.SetSize(80, 20)
	wv.CycleFocus() // focus the worker pane

	// Add 100 lines
	for i := 0; i < 100; i++ {
		wv.AppendAgentOutput("worker", fmt.Sprintf("line-%d", i))
	}

	// Scroll up
	wv.ScrollPane(-10)
	pane := wv.panes["worker"]
	if pane.scrollOffset != 10 {
		t.Errorf("scroll up: offset should be 10, got %d", pane.scrollOffset)
	}

	// Scroll back down
	wv.ScrollPane(5)
	if pane.scrollOffset != 5 {
		t.Errorf("scroll down: offset should be 5, got %d", pane.scrollOffset)
	}

	// Scroll past bottom clamps to 0
	wv.ScrollPane(100)
	if pane.scrollOffset != 0 {
		t.Errorf("scroll past bottom: offset should be 0, got %d", pane.scrollOffset)
	}
}

func TestWorkspace_FocusedPaneRendering(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID: "01FOCUS", Task: "focus", Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"alpha": {Role: "alpha", Backend: "claude", ActivityState: workspace.ActivityActive},
			"beta":  {Role: "beta", Backend: "codex", ActivityState: workspace.ActivityActive},
		},
	}
	wv := NewWorkspaceView(sess)
	wv.SetSize(120, 25)
	wv.CycleFocus() // focus first pane

	output := wv.Render(DefaultTheme)
	plain := stripANSI(output)

	// Focused pane should have ▸ marker
	if !strings.Contains(plain, "▸") {
		t.Errorf("focused pane should show ▸ marker, got:\n%s", plain)
	}
}

func TestWorkspace_UserFriendlyLabels(t *testing.T) {
	tests := []struct {
		state workspace.ActivityState
		want  string
	}{
		{workspace.ActivityActive, "working"},
		{workspace.ActivitySpawning, "starting"},
		{workspace.ActivityExited, "done"},
		{workspace.ActivityBlocked, "STUCK"},
		{workspace.ActivityWaitInput, "needs input"},
	}
	for _, tc := range tests {
		got := activityIcon(tc.state)
		if !strings.Contains(got, tc.want) {
			t.Errorf("activityIcon(%v) = %q, want containing %q", tc.state, got, tc.want)
		}
	}
}

func readOutput(t *testing.T, tm *teatest.TestModel) string {
	t.Helper()
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	out, err := io.ReadAll(tm.FinalOutput(t))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	return string(out)
}
