package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// teamLineTick carries a streaming line from an external agent.
type teamLineTick struct {
	Role string
	Line string
}

// teamDoneTick signals an external agent has finished.
type teamDoneTick struct {
	Role    string
	Elapsed time.Duration
	Error   string
}

// teamAllDoneTick signals all agents have completed.
type teamAllDoneTick struct {
	Results map[string]agent.ExternalAgentResult
}

// startTeamRun launches external agents based on config and enters team view.
func (a *App) startTeamRun(task string) {
	// Auto-detect available backends
	backends := agent.DetectAvailableBackends()

	// If engine has team config, use that to map roles → backends
	if a.engine != nil {
		cfg := a.engine.Config()
		if cfg != nil && cfg.Team != nil {
			var configs []agent.ExternalAgentConfig
			var roles []teamRole
			for roleName, model := range cfg.Team.Models {
				backend := detectBackendFromModel(model.Model)
				timeout := 5 * time.Minute
				if cfg.Team.Default.Timeout > 0 {
					timeout = time.Duration(cfg.Team.Default.Timeout) * time.Second
				}
				configs = append(configs, agent.ExternalAgentConfig{
					Backend: backend,
					Role:    roleName,
					Model:   model.Model,
					Timeout: timeout,
					WorkDir: a.projectRoot,
				})
				roles = append(roles, teamRole{
					Role:    roleName,
					Backend: string(backend),
					Model:   model.Model,
				})
			}
			a.teamView.Start(roles)
			a.appendInfo(fmt.Sprintf("[team] Starting %d agents: %s", len(roles), task))
			a.launchExternalTeam(configs, task)
			return
		}
	}

	// Fallback: auto-detect and assign roles
	if len(backends) == 0 {
		a.appendInfo("[team] No CLI backends found. Install codex, claude, or opencode.")
		return
	}
	a.appendInfo(fmt.Sprintf("[team] Auto-detected %d backend(s)", len(backends)))
	a.runTeamWithBackends(task, backends)
}

// runTeamWithBackends creates agents from auto-detected backends.
func (a *App) runTeamWithBackends(task string, backends []agent.CLIBackend) {
	var configs []agent.ExternalAgentConfig
	var roles []teamRole

	roleNames := []string{"lead", "reviewer", "challenger"}
	for i, b := range backends {
		role := string(b)
		if i < len(roleNames) {
			role = roleNames[i]
		}
		configs = append(configs, agent.ExternalAgentConfig{
			Backend: b,
			Role:    role,
			Timeout: 5 * time.Minute,
			WorkDir: a.projectRoot,
		})
		roles = append(roles, teamRole{
			Role:    role,
			Backend: string(b),
		})
	}

	a.teamView.Start(roles)
	a.appendInfo(fmt.Sprintf("[team] Starting %d agents: %s", len(roles), task))
	a.launchExternalTeam(configs, task)
}

// launchExternalTeam spawns external agents and wires up TUI streaming.
func (a *App) launchExternalTeam(configs []agent.ExternalAgentConfig, task string) {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	streams := agent.SpawnTeam(ctx, configs, task)

	// For each agent, start a goroutine that feeds lines into the TUI
	for role, stream := range streams {
		go a.feedTeamStream(role, stream)
	}
}

// feedTeamStream reads from an external agent stream and sends tea.Msg updates.
func (a *App) feedTeamStream(role string, stream *agent.ExternalAgentStream) {
	// Read lines
	for line := range stream.Lines {
		a.teamView.AppendLine(role, line)
	}
	// Read result
	result := <-stream.Result
	errStr := ""
	if result.Error != nil {
		errStr = result.Error.Error()
	}
	a.teamView.MarkDone(role, result.Elapsed, errStr)

	// If all done, stop team view and append summary
	if a.teamView.AllDone() {
		a.teamView.Stop()
	}
}

// detectBackendFromModel guesses the CLI backend from a model string.
func detectBackendFromModel(model string) agent.CLIBackend {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude") || strings.Contains(m, "anthropic") || strings.Contains(m, "sonnet") || strings.Contains(m, "opus") || strings.Contains(m, "haiku"):
		return agent.BackendClaude
	case strings.Contains(m, "gpt") || strings.Contains(m, "openai") || strings.Contains(m, "o3") || strings.Contains(m, "o4"):
		return agent.BackendCodex
	default:
		return agent.BackendCodex // default to codex for OpenAI-compat
	}
}

// waitForTeamDone returns a tea.Cmd that waits for team completion.
func waitForTeamDone(tv *teamView) tea.Cmd {
	return func() tea.Msg {
		for !tv.AllDone() {
			time.Sleep(100 * time.Millisecond)
		}
		return teamAllDoneTick{}
	}
}
