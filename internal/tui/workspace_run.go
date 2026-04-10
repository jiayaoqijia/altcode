package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/altcode-ai/altcode/internal/workspace/backends"
	tea "github.com/charmbracelet/bubbletea"
)

// agentSpec is a user-specified backend:role pair for workspace agent selection.
type agentSpec struct {
	backend string
	role    string
}

// parseWorkspaceArgs splits the workspace command into task + agent specs.
// "add auth claude:architect codex:coder" → task="add auth", specs=[{claude,architect},{codex,coder}]
// "add auth" → task="add auth", specs=nil (auto-detect)
func parseWorkspaceArgs(parts []string) (string, []agentSpec) {
	var taskParts []string
	var specs []agentSpec
	for _, p := range parts {
		if strings.Contains(p, ":") {
			kv := strings.SplitN(p, ":", 2)
			if len(kv) == 2 && kv[0] != "" && kv[1] != "" {
				specs = append(specs, agentSpec{backend: kv[0], role: kv[1]})
				continue
			}
		}
		taskParts = append(taskParts, p)
	}
	return strings.Join(taskParts, " "), specs
}

// startWorkspaceFromTUIWithAgents starts workspace with optional agent selection.
func (a *App) startWorkspaceFromTUIWithAgents(task string, specs []agentSpec) tea.Cmd {
	if len(specs) == 0 {
		// No specs → auto-detect (original behavior)
		return a.startWorkspaceFromTUI(task)
	}
	// User specified agents — skip detection, build directly
	a.appendInfo(fmt.Sprintf("[workspace] Starting with %d specified agent(s): %s", len(specs), task))

	agents := make(map[string]*workspace.AgentRecord)
	var detected []backends.DetectedBackend
	for _, s := range specs {
		agents[s.role] = &workspace.AgentRecord{
			Role:          s.role,
			Backend:       s.backend,
			ActivityState: workspace.ActivitySpawning,
		}
		detected = append(detected, backends.DetectedBackend{
			Name: s.backend,
		})
	}

	sess := &workspace.WorkspaceSession{
		ID:     fmt.Sprintf("tui-%d", time.Now().UnixMilli()),
		Task:   task,
		Status: workspace.WSSSpawning,
		Agents: agents,
	}

	go a.spawnWorkspaceAgents(sess, detected, task)
	return a.StartWorkspace(sess)
}

// workspaceDetectedMsg carries detected backends from the background probe.
type workspaceDetectedMsg struct {
	detected []backends.DetectedBackend
	task     string
}

// startWorkspaceFromTUI kicks off backend detection in the background
// and shows a "detecting..." message. The actual spawn happens when
// workspaceDetectedMsg arrives in Update.
func (a *App) startWorkspaceFromTUI(task string) tea.Cmd {
	a.appendInfo("[workspace] Detecting agent backends...")
	// Run detection in background — it probes each binary with --version (3s timeout each)
	return func() tea.Msg {
		detected, _ := backends.DetectBackends(context.Background())
		return workspaceDetectedMsg{detected: detected, task: task}
	}
}

// handleWorkspaceDetected is called when backend detection completes.
func (a *App) handleWorkspaceDetected(msg workspaceDetectedMsg) tea.Cmd {
	if len(msg.detected) == 0 {
		a.appendInfo("[workspace] No CLI backends found. Install claude, codex, or opencode.")
		return nil
	}

	agents := make(map[string]*workspace.AgentRecord)
	roleNames := []string{"architect", "implementer", "reviewer"}
	for i, d := range msg.detected {
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
		Task:   msg.task,
		Status: workspace.WSSSpawning,
		Agents: agents,
	}

	a.appendInfo(fmt.Sprintf("[workspace] Starting with %d agent(s): %s", len(msg.detected), msg.task))
	go a.spawnWorkspaceAgents(sess, msg.detected, msg.task)
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
			// Agent exited — use select to avoid blocking forever
			// if the producer closes Events without sending to Result.
			var result agent.ExternalAgentResult
			select {
			case r, ok := <-stream.Result:
				if ok {
					result = r
				}
			case <-time.After(5 * time.Second):
				result = agent.ExternalAgentResult{ExitCode: -1}
			}
			if a.wsView != nil {
				a.wsView.AppendAgentOutput(role, formatAgentExitMessage(result))
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

// spawnAdditionalAgent adds a new agent to an active workspace mid-run.
func (a *App) spawnAdditionalAgent(role, backendName string) tea.Cmd {
	sess := a.wsView.Session()
	if sess == nil {
		a.appendInfo("[spawn] No active session.")
		return nil
	}

	// Check if role already exists
	if a.wsView.HasRole(role) {
		a.appendInfo(fmt.Sprintf("[spawn] Role '%s' already exists. Pick a different name.", role))
		return nil
	}

	// Add agent record to session
	rec := &workspace.AgentRecord{
		Role:          role,
		Backend:       backendName,
		ActivityState: workspace.ActivitySpawning,
	}
	sess.Lock()
	sess.Agents[role] = rec
	sess.Unlock()

	// Add pane to workspace view
	a.wsView.AddAgent(rec)

	a.appendInfo(fmt.Sprintf("[spawn] Spawning %s (%s)...", role, backendName))

	// Spawn the agent in background
	go func() {
		cfg := agent.ExternalAgentConfig{
			Backend: agent.CLIBackend(backendName),
			Role:    role,
			WorkDir: a.projectRoot,
			Timeout: 10 * time.Minute,
		}
		stream := agent.SpawnExternal(context.Background(), cfg, sess.Task)
		go func() {
			for ev := range stream.Events {
				if a.wsView != nil && ev.Content != "" {
					a.wsView.AppendAgentOutput(role, ev.Content)
				}
			}
			var result agent.ExternalAgentResult
			select {
			case r, ok := <-stream.Result:
				if ok {
					result = r
				}
			case <-time.After(5 * time.Second):
				result = agent.ExternalAgentResult{ExitCode: -1}
			}
			if a.wsView != nil {
				a.wsView.AppendAgentOutput(role, formatAgentExitMessage(result))
			}
			sess.Lock()
			rec.ActivityState = workspace.ActivityExited
			rec.ExitCode = result.ExitCode
			now := time.Now()
			rec.ExitedAt = &now
			sess.Unlock()
		}()
	}()

	return nil
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
	allExited := len(sess.Agents) > 0
	for _, rec := range sess.Agents {
		a.wsView.UpdateAgent(rec)
		if rec.Priority() == workspace.AttentionRed {
			bellNeeded = true
		}
		if rec.ActivityState != workspace.ActivityExited {
			allExited = false
		}
	}

	// Populate phase breadcrumb from agent states
	a.wsView.updatePhases()

	// All agents done — unlock input and show summary
	if allExited {
		a.busy = false
		a.appendInfo("[workspace] All agents finished.")
		return nil // stop polling
	}

	// Ring bell at most once per cooldown period
	if bellNeeded && time.Since(a.lastBell) > bellCooldown {
		a.lastBell = time.Now()
		return tea.Batch(
			tea.Printf("\a"),
			a.workspacePollTick(),
		)
	}

	return a.workspacePollTick()
}

// formatAgentExitMessage produces the one-line footer the workspace pane
// shows when an external agent finishes. A normal exit prints just the
// code; a process killed by its own ctx deadline prints a clear
// "[timed out after <d>]" so users don't confuse a timeout with a crash.
func formatAgentExitMessage(r agent.ExternalAgentResult) string {
	if r.TimedOut {
		return fmt.Sprintf("[timed out after %s — bump the backend timeout to run longer]", r.Elapsed.Truncate(time.Second))
	}
	return fmt.Sprintf("[exited: code %d]", r.ExitCode)
}
