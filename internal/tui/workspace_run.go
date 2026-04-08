package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/altcode-ai/altcode/internal/workspace/backends"
	tea "github.com/charmbracelet/bubbletea"
)

// startWorkspaceFromTUI creates a workspace session from a task prompt
// and activates the workspace dashboard. This is the /workspace slash command handler.
func (a *App) startWorkspaceFromTUI(task string) tea.Cmd {
	detected, _ := backends.DetectBackends(context.Background())
	if len(detected) == 0 {
		a.appendInfo("[workspace] No CLI backends found. Install claude, codex, or opencode.")
		return nil
	}

	// Build a lightweight session for the dashboard
	agents := make(map[string]*workspace.AgentRecord)
	roleNames := []string{"architect", "implementer", "reviewer"}
	for i, d := range detected {
		if i >= len(roleNames) {
			break
		}
		agents[roleNames[i]] = &workspace.AgentRecord{
			Role:          roleNames[i],
			Backend:       d.Name,
			ActivityState: workspace.ActivitySpawning,
		}
	}

	sess := &workspace.WorkspaceSession{
		ID:     fmt.Sprintf("tui-%d", time.Now().UnixMilli()),
		Task:   task,
		Status: workspace.WSSSpawning,
		Agents: agents,
	}

	a.appendInfo(fmt.Sprintf("[workspace] Starting with %d agent(s): %s", len(detected), task))
	return a.StartWorkspace(sess)
}

// StartWorkspace activates the workspace dashboard for the given session
// and begins periodic polling.
func (a *App) StartWorkspace(sess *workspace.WorkspaceSession) tea.Cmd {
	a.wsView = NewWorkspaceView(sess)
	a.wsView.SetSize(a.width, max(1, a.height-6))
	a.busy = true
	return a.workspacePollTick()
}

// workspacePollTick returns a tea.Cmd that fires a workspacePollMsg
// after a short delay.
func (a *App) workspacePollTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		return workspacePollMsg{}
	})
}

// bellCooldown prevents the terminal bell from ringing every poll tick.
const bellCooldown = 30 * time.Second

// handleWorkspacePoll refreshes agent panes from the session and checks
// for attention-red conditions (terminal bell with cooldown).
// Returns a cmd to continue polling if the workspace is still active.
func (a *App) handleWorkspacePoll() tea.Cmd {
	if a.wsView == nil || !a.wsView.IsActive() {
		return nil
	}

	sess := a.wsView.sess
	if sess == nil {
		return nil
	}

	bellNeeded := false
	for _, rec := range sess.Agents {
		a.wsView.UpdateAgent(rec)
		if rec.Priority() == workspace.AttentionRed {
			bellNeeded = true
		}
	}

	// Populate phase breadcrumb from agent states (spec gap 3)
	a.wsView.updatePhases()

	// Ring bell at most once per cooldown period
	if bellNeeded && time.Since(a.lastBell) > bellCooldown {
		a.lastBell = time.Now()
		// Use tea.Printf to safely write through Bubbletea's renderer
		return tea.Batch(
			tea.Printf("\a"),
			a.workspacePollTick(),
		)
	}

	return a.workspacePollTick()
}
