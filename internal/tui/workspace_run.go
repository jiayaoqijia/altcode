package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/altcode-ai/altcode/internal/workspace/backends"
	tea "github.com/charmbracelet/bubbletea"
)

// startWorkspaceFromTUI creates a workspace session, spawns real agents,
// and activates the workspace dashboard. This is the /workspace slash command handler.
func (a *App) startWorkspaceFromTUI(task string) tea.Cmd {
	detected, _ := backends.DetectBackends(context.Background())
	if len(detected) == 0 {
		a.appendInfo("[workspace] No CLI backends found. Install claude, codex, or opencode.")
		return nil
	}

	// Build session with agent records
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

	// Spawn real agent processes in background
	go a.spawnWorkspaceAgents(sess, detected, task)

	return a.StartWorkspace(sess)
}

// spawnWorkspaceAgents launches real CLI agents for each role and
// streams their output into the workspace view panes.
func (a *App) spawnWorkspaceAgents(
	sess *workspace.WorkspaceSession,
	detected []backends.DetectedBackend,
	task string,
) {
	ctx := context.Background()
	roleNames := []string{"architect", "implementer", "reviewer"}

	for i, d := range detected {
		if i >= len(roleNames) {
			break
		}
		role := roleNames[i]
		rec := sess.Agents[role]
		if rec == nil {
			continue
		}

		// Spawn the agent process
		cfg := agent.ExternalAgentConfig{
			Backend: agent.CLIBackend(d.Name),
			Role:    role,
			WorkDir: a.projectRoot,
			Timeout: 10 * time.Minute,
		}

		stream := agent.SpawnExternal(ctx, cfg, task)

		// Stream output lines into the TUI pane
		go func(role string, stream *agent.ExternalAgentStream) {
			for ev := range stream.Events {
				if a.wsView != nil && ev.Content != "" {
					a.wsView.AppendAgentOutput(role, ev.Content)
				}
			}
			// Agent exited
			result := <-stream.Result
			if a.wsView != nil {
				a.wsView.AppendAgentOutput(role,
					fmt.Sprintf("[exited: code %d]", result.ExitCode))
			}
			if rec != nil {
				sess.Lock()
				rec.ActivityState = workspace.ActivityExited
				rec.ExitCode = result.ExitCode
				now := time.Now()
				rec.ExitedAt = &now
				sess.Unlock()
			}
		}(role, stream)
	}
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

	sess := a.wsView.Session()
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
