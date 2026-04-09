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
	active    bool
	width     int
	height    int
	paused    bool
	inputHas  string // current input prefix (for Tab hint accuracy)
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

// AddAgent adds a new agent pane to the workspace view (for /spawn mid-run).
func (wv *WorkspaceView) AddAgent(rec *workspace.AgentRecord) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	if _, exists := wv.panes[rec.Role]; exists {
		return
	}
	pane := &wsAgentPane{
		Role:     rec.Role,
		Backend:  rec.Backend,
		Activity: rec.ActivityState,
		Priority: rec.Priority(),
	}
	wv.panes[rec.Role] = pane
	wv.order = append(wv.order, rec.Role)
	sort.Strings(wv.order)
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

// FocusByClick determines which pane was clicked based on coordinates.
// Handles both horizontal layout (X-based) and vertical layout (Y-based).
func (wv *WorkspaceView) FocusByClick(x, y int) bool {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	n := len(wv.order)
	if n == 0 {
		return false
	}

	// Vertical stack mode: 4+ agents on narrow terminal
	if n > 3 && wv.width < 120 {
		paneH := (wv.height - 4) / n
		if paneH < 3 {
			paneH = 3
		}
		idx := (y - 1) / paneH // -1 for header
		if idx >= n {
			idx = n - 1
		}
		if idx < 0 {
			idx = 0
		}
		wv.focus = idx
		return true
	}

	// Horizontal layout: divide by pane width
	totalW := wv.width
	if totalW < 40 {
		totalW = 40
	}
	paneW := (totalW - (n - 1)) / n
	if paneW < 20 {
		paneW = 20
	}
	idx := x / paneW
	if idx >= n {
		idx = n - 1
	}
	if idx < 0 {
		idx = 0
	}
	wv.focus = idx
	return true
}

// ScrollPane scrolls the focused pane's output by delta lines.
// Negative delta = scroll up (show older lines), positive = scroll down.
func (wv *WorkspaceView) ScrollPane(delta int) {
	wv.mu.Lock()
	defer wv.mu.Unlock()
	if wv.focus < 0 || wv.focus >= len(wv.order) {
		return
	}
	p := wv.panes[wv.order[wv.focus]]
	if p == nil {
		return
	}
	// Invert: negative delta = scroll up = increase offset (older lines)
	p.scrollOffset -= delta
	if p.scrollOffset < 0 {
		p.scrollOffset = 0
	}
	maxScroll := len(p.Lines) - 10
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scrollOffset > maxScroll {
		p.scrollOffset = maxScroll
	}
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

	// Snapshot sess fields — caller holds wv.mu but sess itself
	// may be written concurrently by the lifecycle goroutine.
	// These reads are safe because Bubbletea renders on the main
	// goroutine and sess mutations only happen via handleWorkspacePoll
	// which also runs on the main goroutine (tea.Cmd/Msg model).
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

	// Set focus state on panes
	for i, role := range wv.order {
		wv.panes[role].Focused = (i == wv.focus)
	}

	var rendered []string
	for _, role := range wv.order {
		p := wv.panes[role]
		rendered = append(rendered, p.Render(theme, paneW, paneH))
	}

	// Stack vertically when too many agents for horizontal layout
	if n > 3 && totalW < 120 {
		return lipgloss.JoinVertical(lipgloss.Left, rendered...)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// renderFooter shows contextual key hints based on current state.
func (wv *WorkspaceView) renderFooter(theme Theme) string {
	hint := lipgloss.NewStyle().Foreground(theme.Muted)
	key := lipgloss.NewStyle().Foreground(theme.Warning).Bold(true)

	// Context-sensitive hints — show what's relevant NOW
	var parts []string
	if wv.paused {
		parts = append(parts, key.Render("Ctrl+R")+" "+hint.Render("resume"))
		parts = append(parts, key.Render("Ctrl+Q")+" "+hint.Render("abort"))
	} else {
		// Tab hint changes based on input: slash-complete vs focus cycling
		if strings.HasPrefix(wv.inputHas, "/") {
			parts = append(parts, key.Render("Tab")+" "+hint.Render("complete"))
		} else {
			parts = append(parts, key.Render("Tab")+" "+hint.Render("focus"))
		}
		if wv.focus >= 0 {
			parts = append(parts, key.Render("Ctrl+S")+" "+hint.Render("send"))
		}
		parts = append(parts, key.Render("Ctrl+Z")+" "+hint.Render("pause"))
		parts = append(parts, key.Render("Ctrl+Q")+" "+hint.Render("abort"))
	}

	// Status with focused agent name
	status := "working"
	if wv.paused {
		status = "PAUSED"
	}
	focusInfo := ""
	if wv.focus >= 0 && wv.focus < len(wv.order) {
		focusInfo = " → " + wv.order[wv.focus]
	}
	statusStyle := lipgloss.NewStyle().
		Foreground(theme.Warning).Bold(true).
		Render(fmt.Sprintf("[%s%s]", status, focusInfo))

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
