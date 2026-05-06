package daemon

import (
	"encoding/json"
	"strings"
)

// WorkspaceMode determines how many agents are spawned for a task.
type WorkspaceMode string

const (
	ModeSolo WorkspaceMode = "solo" // 1 agent (codex)
	ModePair WorkspaceMode = "pair" // lead + implementer
	ModeTeam WorkspaceMode = "team" // lead + implementer + reviewer + tester
)

// RepoProfile holds stats for complexity estimation.
type RepoProfile struct {
	TotalFiles int
	TotalLOC   int
	Languages  []string
	HasTests   bool
	HasCI      bool
}

// ModelSet maps agent roles to model identifiers.
type ModelSet struct {
	Lead     string // e.g. "anthropic/claude-sonnet-4"
	Impl     string // e.g. "codex"
	Reviewer string
	Tester   string
	Fallback string
}

// RoutingConfig holds model assignments per complexity tier.
type RoutingConfig struct {
	Simple  ModelSet
	Medium  ModelSet
	Complex ModelSet
}

// SelectMode auto-selects workspace mode based on repo profile
// and estimated number of files the task will touch.
func SelectMode(profile *RepoProfile, estimatedFiles int) WorkspaceMode {
	if profile.TotalLOC < 500 && estimatedFiles < 3 {
		return ModeSolo
	}
	if estimatedFiles < 10 {
		return ModePair
	}
	return ModeTeam
}

// EstimateComplexity classifies task complexity as
// "simple", "medium", or "complex".
func EstimateComplexity(
	profile *RepoProfile, taskDescription string,
) string {
	if profile.TotalLOC < 500 {
		return "simple"
	}
	complexSignals := []string{
		"refactor", "architecture", "migrate",
		"redesign", "overhaul",
	}
	lower := strings.ToLower(taskDescription)
	for _, sig := range complexSignals {
		if strings.Contains(lower, sig) {
			return "complex"
		}
	}
	if profile.TotalLOC > 10000 {
		return "complex"
	}
	return "medium"
}

// DefaultRoutingConfig returns the default model assignments
// per complexity tier.
func DefaultRoutingConfig() RoutingConfig {
	return RoutingConfig{
		Simple: ModelSet{
			Lead: "codex",
			Impl: "codex",
		},
		Medium: ModelSet{
			Lead:     "minimax/MiniMax-M2.7",
			Impl:     "codex",
			Reviewer: "kimi/kimi-k2",
			Tester:   "codex",
		},
		Complex: ModelSet{
			Lead:     "anthropic/claude-sonnet-4",
			Impl:     "codex",
			Reviewer: "anthropic/claude-sonnet-4",
			Tester:   "codex",
			Fallback: "anthropic/claude-opus-4-6",
		},
	}
}

// RouteModels selects a ModelSet based on complexity string.
func RouteModels(complexity string, cfg RoutingConfig) ModelSet {
	switch complexity {
	case "simple":
		return cfg.Simple
	case "complex":
		return cfg.Complex
	default:
		return cfg.Medium
	}
}

// AgentsForMode returns the agent roles needed for a mode.
func AgentsForMode(mode WorkspaceMode) []string {
	switch mode {
	case ModeSolo:
		return []string{"solo"}
	case ModePair:
		return []string{"lead", "implementer"}
	default:
		return []string{"lead", "implementer", "reviewer", "tester"}
	}
}

// taskAgentOverride is the in-memory shape encoded into Task.AgentConfig
// so the orchestrator can pick up per-task overrides without a schema
// change. Blank fields fall back to server-level defaults.
type taskAgentOverride struct {
	Mode  string `json:"mode,omitempty"`  // "solo"|"pair"|"team"
	Model string `json:"model,omitempty"` // per-task model override
}

// encodeAgentConfig serialises per-task overrides as JSON. Returns
// the legacy plain-string form when only a mode is provided so
// existing tasks round-trip unchanged.
func encodeAgentConfig(mode, model string) string {
	if mode == "" && model == "" {
		return ""
	}
	if model == "" {
		return mode
	}
	b, _ := json.Marshal(taskAgentOverride{Mode: mode, Model: model})
	return string(b)
}

// decodeAgentConfig parses Task.AgentConfig back into (mode, model).
// Tolerates the legacy plain-string form where only mode was stored.
func decodeAgentConfig(raw string) (mode, model string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if !strings.HasPrefix(raw, "{") {
		return raw, ""
	}
	var o taskAgentOverride
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return raw, ""
	}
	return o.Mode, o.Model
}
