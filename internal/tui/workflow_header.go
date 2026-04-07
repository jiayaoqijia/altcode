package tui

import (
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/orchestra"
	"github.com/charmbracelet/lipgloss"
)

type workflowHeader struct {
	phases []phaseDisplay
	width  int
}

type phaseDisplay struct {
	Name    string
	Verdict orchestra.Verdict
	Active  bool
}

func (wh *workflowHeader) SetPhases(names []string) {
	wh.phases = make([]phaseDisplay, len(names))
	for i, n := range names {
		wh.phases[i] = phaseDisplay{Name: n}
	}
}

func (wh *workflowHeader) MarkActive(name string) {
	for i := range wh.phases {
		wh.phases[i].Active = wh.phases[i].Name == name
	}
}

func (wh *workflowHeader) MarkDone(name string, verdict orchestra.Verdict) {
	for i := range wh.phases {
		if wh.phases[i].Name == name {
			wh.phases[i].Verdict = verdict
			wh.phases[i].Active = false
		}
	}
}

func (wh *workflowHeader) Render(theme Theme) string {
	if len(wh.phases) == 0 {
		return ""
	}
	var parts []string
	for _, p := range wh.phases {
		icon := "·"
		color := theme.Muted
		switch {
		case p.Active:
			icon = "⟳"
			color = theme.Warning
		case p.Verdict == orchestra.VerdictPass:
			icon = "✓"
			color = theme.Success
		case p.Verdict == orchestra.VerdictFail:
			icon = "✗"
			color = theme.Error
		case p.Verdict == orchestra.VerdictSkipped:
			icon = "⊘"
			color = theme.Muted
		}
		badge := lipgloss.NewStyle().Foreground(color).Render(
			fmt.Sprintf("[%s %s]", p.Name, icon))
		parts = append(parts, badge)
	}
	sep := lipgloss.NewStyle().Foreground(theme.Muted).Render(" → ")
	return lipgloss.NewStyle().Width(wh.width).Render("  " + strings.Join(parts, sep))
}
