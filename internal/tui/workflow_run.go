package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/altcode-ai/altcode/internal/orchestra"
	"github.com/altcode-ai/altcode/internal/wfdef"
	tea "github.com/charmbracelet/bubbletea"
)

// handleWorkflowEvent processes a PhaseEvent from the orchestra.
func (a *App) handleWorkflowEvent(ev orchestra.PhaseEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case orchestra.KindPhaseDone:
		// Phase completed — update header and transition panes
		verdict := orchestra.VerdictPass
		if ev.Text == "fail" {
			verdict = orchestra.VerdictFail
		} else if strings.Contains(ev.Text, "skipped") {
			verdict = orchestra.VerdictSkipped
		}
		a.wfHeader.MarkDone(ev.Phase, verdict)
		a.appendInfo(fmt.Sprintf("[%s] %s", ev.Phase, ev.Text))
		a.updateViewport()
		return a, a.waitForWfEvent()

	case orchestra.KindToolStart:
		a.teamView.AppendLine(ev.Role, fmt.Sprintf("⟳ %s %s", ev.Tool, ev.Text))
		a.updateViewport()
		return a, a.waitForWfEvent()

	case orchestra.KindToolDone:
		a.teamView.AppendLine(ev.Role, fmt.Sprintf("✓ %s", ev.Text))
		a.updateViewport()
		return a, a.waitForWfEvent()

	case orchestra.KindError:
		a.teamView.AppendLine(ev.Role, fmt.Sprintf("✗ %s", ev.Text))
		a.appendInfo(fmt.Sprintf("[%s] error: %s", ev.Phase, ev.Text))
		a.updateViewport()
		return a, a.waitForWfEvent()

	case orchestra.KindThinking:
		// Don't flood TUI with thinking — just mark the role active
		a.wfHeader.MarkActive(ev.Phase)
		a.updateViewport()
		return a, a.waitForWfEvent()

	default: // KindText
		if ev.Text != "" {
			a.teamView.AppendLine(ev.Role, ev.Text)
			a.updateViewport()
		}
		return a, a.waitForWfEvent()
	}
}

// waitForWfEvent returns a tea.Cmd that reads the next workflow event.
func (a *App) waitForWfEvent() tea.Cmd {
	if a.wfEvents == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-a.wfEvents
		if !ok {
			return wfDoneMsg{}
		}
		return wfEventMsg(ev)
	}
}

// startWorkflowRun loads a workflow definition and runs it via the orchestra.
// Returns a tea.Cmd to start polling workflow events.
func (a *App) startWorkflowRun(def *wfdef.WorkflowDef, task string) tea.Cmd {
	// Set up phase header
	order, err := def.TopoSort()
	if err != nil {
		a.appendInfo(fmt.Sprintf("[workflow] topo sort error: %v", err))
		return nil
	}
	a.wfHeader.SetPhases(order)
	a.wfHeader.SetWidth(a.width)

	// Initialize team view with first phase's agents
	if first := def.PhaseByName(order[0]); first != nil {
		var roles []teamRole
		for _, ag := range first.Agents {
			roles = append(roles, teamRole{Role: ag.Role, Backend: ag.Backend, Model: ag.Model})
		}
		a.teamView.Start(roles)
	}
	a.wfHeader.MarkActive(order[0])

	// Create event channels
	events := make(chan orchestra.PhaseEvent, 500)
	a.wfEvents = events
	a.wfRunning = true
	a.busy = true

	// Launch orchestra in background
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	workDir := a.projectRoot
	if workDir == "" {
		workDir = "."
	}

	go func() {
		defer close(events)
		err := orchestra.Run(ctx, orchestra.RunParams{
			Def:      def,
			Task:     task,
			WorkDir:  workDir,
			Events:   events,
			Override: a.wfOverride,
		})
		if err != nil {
			select {
			case events <- orchestra.PhaseEvent{Type: orchestra.KindError, Text: err.Error()}:
			default:
			}
		}
	}()

	a.appendInfo(fmt.Sprintf("[workflow] Starting %q: %s", def.Name, task))

	// Return cmd to start polling workflow events
	return a.waitForWfEvent()
}

// discoverAndRunWorkflow looks up a workflow by name and runs it.
// Returns a tea.Cmd to start polling events, or nil if not found.
func (a *App) discoverAndRunWorkflow(name, task string) tea.Cmd {
	dirs := []string{
		filepath.Join(a.projectRoot, ".altcode", "workflows"),
	}
	if home, err := homeConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "workflows"))
	}

	defs, err := wfdef.Discover(dirs...)
	if err != nil {
		a.appendInfo(fmt.Sprintf("[workflow] discover error: %v", err))
		return nil
	}

	for _, def := range defs {
		if def.Name == name {
			return a.startWorkflowRun(def, task)
		}
	}

	if len(defs) == 0 {
		a.appendInfo("[workflow] No workflow definitions found in .altcode/workflows/")
	} else {
		var names []string
		for _, d := range defs {
			names = append(names, d.Name)
		}
		a.appendInfo(fmt.Sprintf("[workflow] %q not found. Available: %s", name, strings.Join(names, ", ")))
	}
	return nil
}

func homeConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "altcode"), nil
}
