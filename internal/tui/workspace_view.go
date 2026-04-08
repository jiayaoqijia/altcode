package tui

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/charmbracelet/lipgloss"
)

// WorkspaceView renders the workspace mode dashboard with agent panes
// and a phase breadcrumb header. It is embedded in the main App and
// activated when a workspace session is running.
type WorkspaceView struct {
	mu     sync.Mutex
	sess   *workspace.WorkspaceSession
	panes  map[string]*wsAgentPane // keyed by role
	order  []string                // role display order
	phases []wsPhase               // phase breadcrumb
	focus  int                     // focused pane index (-1 = none)
	active bool
	width  int
	height int
	paused bool
}

// wsPhase tracks one phase in the breadcrumb bar.
type wsPhase struct {
	Name   string
	Status string // "done", "running", "pending", "failed"
}

// NewWorkspaceView creates a workspace view for the given session.
func NewWorkspaceView(sess *workspace.WorkspaceSession) *WorkspaceView {
	wv := &WorkspaceView{
		sess:  sess,
		panes: make(map[string]*wsAgentPane),
		focus: -1,
	}
	// Build stable role order (sorted) to avoid non-deterministic map iteration
	roles := make([]string, 0, len(sess.Agents))
	for role := range sess.Agents {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		rec := sess.Agents[role]
		pane := &wsAgentPane{
			Role:     rec.Role,
			Backend:  rec.Backend,
			Activity: rec.ActivityState,
			Branch:   rec.Branch,
			PRID:     rec.PRID,
			CIStatus: rec.CIStatus,
			Priority: rec.Priority(),
			CostUSD:  rec.CostUSD,
			Turns:    rec.TurnCount,
		}
		wv.panes[role] = pane
		wv.order = append(wv.order, role)
	}
	wv.active = true
	return wv
}

// IsActive reports whether the workspace view should be displayed.
func (wv *WorkspaceView) IsActive() bool {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	return wv.active
}

// Stop deactivates the workspace view.
func (wv *WorkspaceView) Stop() {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	wv.active = false
}

// SetSize updates the render dimensions.
func (wv *WorkspaceView) SetSize(w, h int) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	wv.width = w
	wv.height = h
}

// FocusAgent sets focus to the Nth agent pane (0-indexed).
func (wv *WorkspaceView) FocusAgent(n int) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	if n >= 0 && n < len(wv.order) {
		wv.focus = n
	}
}

// Session returns the underlying workspace session (mutex-protected).
func (wv *WorkspaceView) Session() *workspace.WorkspaceSession {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	return wv.sess
}

// HasRole checks if a role exists in this workspace view.
func (wv *WorkspaceView) HasRole(role string) bool {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	_, ok := wv.panes[role]
	return ok
}

// FocusedRole returns the role name of the currently focused agent, or "".
func (wv *WorkspaceView) FocusedRole() string {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	if wv.focus >= 0 && wv.focus < len(wv.order) {
		return wv.order[wv.focus]
	}
	return ""
}

// CycleFocus advances focus to the next pane.
func (wv *WorkspaceView) CycleFocus() {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	if len(wv.order) == 0 {
		return
	}
	wv.focus = (wv.focus + 1) % len(wv.order)
}

// SetPaused updates the paused state for display.
func (wv *WorkspaceView) SetPaused(p bool) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	wv.paused = p
}

// AppendAgentOutput adds output text to the named agent's pane.
func (wv *WorkspaceView) AppendAgentOutput(role, text string) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	if p, ok := wv.panes[role]; ok {
		for _, line := range strings.Split(text, "\n") {
			p.AppendOutput(line)
		}
	}
}

// UpdateAgent refreshes an agent pane from its current record.
func (wv *WorkspaceView) UpdateAgent(rec *workspace.AgentRecord) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	p, ok := wv.panes[rec.Role]
	if !ok {
		return
	}
	p.Activity = rec.ActivityState
	p.PRID = rec.PRID
	p.CIStatus = rec.CIStatus
	p.Priority = rec.Priority()
	p.CostUSD = rec.CostUSD
	p.Turns = rec.TurnCount
}

// updatePhases rebuilds the phase breadcrumb from current agent states.
func (wv *WorkspaceView) updatePhases() {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	phases := make([]wsPhase, 0, len(wv.order))
	for _, role := range wv.order {
		p := wv.panes[role]
		status := "pending"
		switch p.Activity {
		case workspace.ActivityActive:
			status = "running"
		case workspace.ActivityExited:
			status = "done"
		case workspace.ActivityBlocked:
			status = "failed"
		case workspace.ActivityReady, workspace.ActivityIdle:
			status = "done"
		}
		phases = append(phases, wsPhase{
			Name:   role,
			Status: status,
		})
	}
	wv.phases = phases
}

// Render returns the full workspace dashboard view.
func (wv *WorkspaceView) Render(theme Theme) string {
	wv.mu.Lock()
	defer wv.mu.Unlock()

	if len(wv.order) == 0 {
		return lipgloss.NewStyle().Foreground(theme.Muted).
			Render("  No agents spawned yet.")
	}

	headerLine := wv.renderHeader(theme)
	paneArea := wv.renderPanes(theme)
	footer := wv.renderFooter(theme)

	return headerLine + "\n" + paneArea + "\n" + footer
}

// renderHeader builds the workspace status bar with phase breadcrumb.
func (wv *WorkspaceView) renderHeader(theme Theme) string {
	var parts []string

	// Workspace ID and task
	id := wv.sess.ID
	if len(id) > 8 {
		id = id[:8]
	}
	label := lipgloss.NewStyle().
		Foreground(theme.Primary).Bold(true).
		Render(fmt.Sprintf("[workspace:%s]", id))
	task := lipgloss.NewStyle().
		Foreground(theme.Foreground).
		Render(truncateStr(wv.sess.Task, 40))
	status := lipgloss.NewStyle().
		Foreground(theme.Warning).
		Render(string(wv.sess.Status))
	parts = append(parts, label, task, status)

	// Aggregate CI + PR info from agents
	var prCount, ciPassCount, ciFail int
	for _, p := range wv.panes {
		if p.PRID > 0 {
			prCount++
		}
		if p.CIStatus == workspace.CIPass {
			ciPassCount++
		} else if p.CIStatus == workspace.CIFail {
			ciFail++
		}
	}
	if prCount > 0 {
		prInfo := fmt.Sprintf("pr:%d", prCount)
		if ciFail > 0 {
			prInfo += fmt.Sprintf(" ci:%d fail", ciFail)
		} else if ciPassCount > 0 {
			prInfo += " ci:pass"
		}
		parts = append(parts, lipgloss.NewStyle().
			Foreground(theme.Muted).Render(prInfo))
	}

	// Phase breadcrumb
	if len(wv.phases) > 0 {
		var badges []string
		for _, ph := range wv.phases {
			icon, color := phaseIcon(ph.Status, theme)
			b := lipgloss.NewStyle().Foreground(color).
				Render(fmt.Sprintf("[%s %s]", ph.Name, icon))
			badges = append(badges, b)
		}
		sep := lipgloss.NewStyle().Foreground(theme.Muted).
			Render(" → ")
		parts = append(parts, strings.Join(badges, sep))
	}

	return lipgloss.NewStyle().Width(wv.width).
		Render(strings.Join(parts, "  "))
}

// renderPanes lays out agent panes side by side.
func (wv *WorkspaceView) renderPanes(theme Theme) string {
	n := len(wv.order)
	if n == 0 {
		return ""
	}

	totalW := wv.width
	if totalW < 40 {
		totalW = 40
	}
	paneW := (totalW - (n - 1)) / n
	if paneW < 20 {
		paneW = 20
	}
	paneH := wv.height - 4 // header + footer + borders
	if paneH < 6 {
		paneH = 6
	}

	var rendered []string
	for _, role := range wv.order {
		p := wv.panes[role]
		rendered = append(rendered, p.Render(theme, paneW, paneH))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// renderFooter shows key hints and overall status.
func (wv *WorkspaceView) renderFooter(theme Theme) string {
	hint := lipgloss.NewStyle().Foreground(theme.Muted)
	key := lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)

	parts := []string{
		key.Render("Ctrl+Z") + hint.Render(" pause"),
		key.Render("Ctrl+Q") + hint.Render(" abort"),
		key.Render("Ctrl+S") + hint.Render(" send"),
		key.Render("Tab") + hint.Render(" cycle"),
	}

	status := "working"
	if wv.paused {
		status = "PAUSED"
	}
	statusStyle := lipgloss.NewStyle().
		Foreground(theme.Warning).Bold(true).
		Render(fmt.Sprintf("[%s]", status))

	return lipgloss.NewStyle().Width(wv.width).Render(
		statusStyle + "  " + strings.Join(parts, "  "))
}

// phaseIcon returns an icon and color for a phase status string.
func phaseIcon(status string, theme Theme) (string, lipgloss.TerminalColor) {
	switch status {
	case "done":
		return "✓", theme.Success
	case "running":
		return "⟳", theme.Warning
	case "failed":
		return "✗", theme.Error
	default:
		return "·", theme.Muted
	}
}
