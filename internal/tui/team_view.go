package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// agentPaneStatus tracks an external agent's display state.
type agentPaneStatus int

const (
	paneRunning   agentPaneStatus = iota
	paneSucceeded
	paneFailed
	paneCancelled
)

// agentPane holds the display state for one agent in the team view.
type agentPane struct {
	Role    string
	Backend string // "codex", "claude", "opencode"
	Model   string
	Status  agentPaneStatus
	Lines   []string // rolling output buffer
	Elapsed time.Duration
	Error   string
}

// teamView manages the split-pane display for multi-agent workflows.
type teamView struct {
	mu     sync.Mutex
	panes  []*agentPane
	active bool
	width  int
	height int
}

func newTeamView() *teamView {
	return &teamView{}
}

// Start initializes the team view with the given agent roles.
func (tv *teamView) Start(roles []teamRole) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.panes = make([]*agentPane, len(roles))
	for i, r := range roles {
		tv.panes[i] = &agentPane{
			Role:    r.Role,
			Backend: r.Backend,
			Model:   r.Model,
			Status:  paneRunning,
		}
	}
	tv.active = true
}

// teamRole describes a role for display purposes.
type teamRole struct {
	Role    string
	Backend string
	Model   string
}

// AppendLine adds an output line to the given role's pane.
func (tv *teamView) AppendLine(role, line string) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	for _, p := range tv.panes {
		if p.Role == role {
			p.Lines = append(p.Lines, line)
			// Keep last 50 lines for display
			if len(p.Lines) > 50 {
				p.Lines = p.Lines[len(p.Lines)-50:]
			}
			return
		}
	}
}

// MarkDone marks a role as completed (success or failure).
func (tv *teamView) MarkDone(role string, elapsed time.Duration, err string) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	for _, p := range tv.panes {
		if p.Role == role {
			p.Elapsed = elapsed
			if err != "" {
				p.Status = paneFailed
				p.Error = err
			} else {
				p.Status = paneSucceeded
			}
			return
		}
	}
}

// IsActive returns whether the team view is currently running.
func (tv *teamView) IsActive() bool {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	return tv.active
}

// Stop deactivates the team view.
func (tv *teamView) Stop() {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.active = false
}

// AllDone returns true if every pane has finished.
func (tv *teamView) AllDone() bool {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	for _, p := range tv.panes {
		if p.Status == paneRunning {
			return false
		}
	}
	return true
}

// SetSize updates the available render dimensions.
func (tv *teamView) SetSize(w, h int) {
	tv.mu.Lock()
	defer tv.mu.Unlock()
	tv.width = w
	tv.height = h
}

// Render returns the full split-pane view as a string.
func (tv *teamView) Render(theme Theme) string {
	tv.mu.Lock()
	defer tv.mu.Unlock()

	if len(tv.panes) == 0 {
		return ""
	}

	n := len(tv.panes)
	totalWidth := tv.width
	if totalWidth < 40 {
		totalWidth = 40
	}

	// Calculate pane dimensions, ensuring total doesn't exceed terminal width
	separators := n - 1
	paneWidth := (totalWidth - separators) / n
	if paneWidth < 20 {
		paneWidth = 20
	}
	// Clamp: if panes + separators exceed terminal, reduce pane count
	for n > 1 && (paneWidth*n+separators) > totalWidth {
		n--
		separators = n - 1
		paneWidth = (totalWidth - separators) / n
	}
	paneHeight := tv.height - 2 // header + footer
	if paneHeight < 5 {
		paneHeight = 5
	}

	var rendered []string
	for _, p := range tv.panes {
		rendered = append(rendered, tv.renderPane(p, paneWidth, paneHeight, theme))
	}

	// Join panes horizontally
	sep := lipgloss.NewStyle().
		Foreground(theme.Border).
		Render(strings.Repeat("│\n", paneHeight))

	return lipgloss.JoinHorizontal(lipgloss.Top, interleave(rendered, sep)...)
}

// renderPane renders a single agent's pane.
func (tv *teamView) renderPane(p *agentPane, w, h int, theme Theme) string {
	// Header: role badge + backend + status
	var statusIcon string
	var statusColor lipgloss.Color
	switch p.Status {
	case paneRunning:
		statusIcon = "⟳"
		statusColor = theme.Warning
	case paneSucceeded:
		statusIcon = "✓"
		statusColor = theme.Success
	case paneFailed:
		statusIcon = "✗"
		statusColor = theme.Error
	case paneCancelled:
		statusIcon = "⊘"
		statusColor = theme.Muted
	}

	badge := lipgloss.NewStyle().
		Background(statusColor).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1).
		Render(p.Role)

	backend := lipgloss.NewStyle().
		Foreground(theme.Muted).
		Render(p.Backend)

	elapsed := ""
	if p.Elapsed > 0 {
		elapsed = lipgloss.NewStyle().
			Foreground(theme.Muted).
			Render(fmt.Sprintf(" %s", p.Elapsed.Truncate(time.Millisecond)))
	}

	header := fmt.Sprintf("%s %s %s%s", statusIcon, badge, backend, elapsed)

	// Body: last N lines of output
	visibleLines := h - 1 // reserve 1 for header
	if visibleLines < 1 {
		visibleLines = 1
	}

	lines := p.Lines
	if len(lines) > visibleLines {
		lines = lines[len(lines)-visibleLines:]
	}

	// Truncate long lines (rune-safe for UTF-8)
	var body []string
	maxW := w - 2
	if maxW < 3 {
		maxW = 3
	}
	for _, l := range lines {
		runes := []rune(l)
		if len(runes) > maxW {
			cut := maxW - 3
			if cut < 0 {
				cut = 0
			}
			l = string(runes[:cut]) + "..."
		}
		body = append(body, l)
	}
	// Pad to fill height
	for len(body) < visibleLines {
		body = append(body, "")
	}

	content := strings.Join(body, "\n")

	// Error line at bottom
	if p.Error != "" {
		errLine := lipgloss.NewStyle().
			Foreground(theme.Error).
			Render("err: " + truncateStr(p.Error, w-6))
		content = content + "\n" + errLine
	}

	// Box the pane
	return lipgloss.NewStyle().
		Width(w).
		Height(h + 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(statusColor).
		Render(header + "\n" + content)
}

func interleave(items []string, sep string) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			result = append(result, sep)
		}
		result = append(result, item)
	}
	return result
}
