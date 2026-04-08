package tui

import (
	"fmt"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

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

// handleWorkspacePoll refreshes agent panes from the session and checks
// for attention-red conditions (terminal bell). Returns a cmd to
// continue polling if the workspace is still active.
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

	if bellNeeded {
		fmt.Print("\a") // terminal bell for urgent attention
	}

	return a.workspacePollTick()
}
