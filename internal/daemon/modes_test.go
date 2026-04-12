package daemon

import (
	"testing"
)

func TestSelectMode_Solo(t *testing.T) {
	p := &RepoProfile{TotalLOC: 200, TotalFiles: 5}
	got := SelectMode(p, 2)
	if got != ModeSolo {
		t.Errorf("SelectMode = %q, want %q", got, ModeSolo)
	}
}

func TestSelectMode_Pair(t *testing.T) {
	p := &RepoProfile{TotalLOC: 3000, TotalFiles: 40}
	got := SelectMode(p, 7)
	if got != ModePair {
		t.Errorf("SelectMode = %q, want %q", got, ModePair)
	}
}

func TestSelectMode_Team(t *testing.T) {
	p := &RepoProfile{TotalLOC: 20000, TotalFiles: 200}
	got := SelectMode(p, 15)
	if got != ModeTeam {
		t.Errorf("SelectMode = %q, want %q", got, ModeTeam)
	}
}

func TestEstimateComplexity_Simple(t *testing.T) {
	p := &RepoProfile{TotalLOC: 100}
	got := EstimateComplexity(p, "add a helper function")
	if got != "simple" {
		t.Errorf("EstimateComplexity = %q, want %q", got, "simple")
	}
}

func TestEstimateComplexity_Medium(t *testing.T) {
	p := &RepoProfile{TotalLOC: 5000}
	got := EstimateComplexity(p, "add a new endpoint")
	if got != "medium" {
		t.Errorf("EstimateComplexity = %q, want %q", got, "medium")
	}
}

func TestEstimateComplexity_Complex(t *testing.T) {
	p := &RepoProfile{TotalLOC: 15000}
	got := EstimateComplexity(p, "add a new endpoint")
	if got != "complex" {
		t.Errorf("EstimateComplexity = %q, want %q", got, "complex")
	}
}

func TestEstimateComplexity_ComplexKeywords(t *testing.T) {
	keywords := []string{
		"refactor the auth module",
		"Architecture overhaul needed",
		"Migrate to new API",
		"Redesign the dashboard",
		"Overhaul the pipeline",
	}
	p := &RepoProfile{TotalLOC: 5000}
	for _, desc := range keywords {
		got := EstimateComplexity(p, desc)
		if got != "complex" {
			t.Errorf("EstimateComplexity(%q) = %q, want %q",
				desc, got, "complex")
		}
	}
}

func TestRouteModels_Simple(t *testing.T) {
	cfg := DefaultRoutingConfig()
	ms := RouteModels("simple", cfg)
	if ms.Lead != "codex" || ms.Impl != "codex" {
		t.Errorf("simple routing: Lead=%q Impl=%q", ms.Lead, ms.Impl)
	}
}

func TestRouteModels_Medium(t *testing.T) {
	cfg := DefaultRoutingConfig()
	ms := RouteModels("medium", cfg)
	if ms.Lead != "minimax/MiniMax-M2.7" {
		t.Errorf("medium Lead = %q, want minimax/MiniMax-M2.7", ms.Lead)
	}
	if ms.Reviewer != "kimi/kimi-k2" {
		t.Errorf("medium Reviewer = %q, want kimi/kimi-k2", ms.Reviewer)
	}
}

func TestRouteModels_Complex(t *testing.T) {
	cfg := DefaultRoutingConfig()
	ms := RouteModels("complex", cfg)
	if ms.Lead != "anthropic/claude-sonnet-4" {
		t.Errorf("complex Lead = %q", ms.Lead)
	}
	if ms.Fallback != "anthropic/claude-opus-4-6" {
		t.Errorf("complex Fallback = %q", ms.Fallback)
	}
}

func TestAgentsForMode(t *testing.T) {
	tests := []struct {
		mode WorkspaceMode
		want []string
	}{
		{ModeSolo, []string{"solo"}},
		{ModePair, []string{"lead", "implementer"}},
		{ModeTeam, []string{"lead", "implementer", "reviewer", "tester"}},
	}
	for _, tt := range tests {
		got := AgentsForMode(tt.mode)
		if len(got) != len(tt.want) {
			t.Errorf("AgentsForMode(%q) len=%d, want %d",
				tt.mode, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("AgentsForMode(%q)[%d] = %q, want %q",
					tt.mode, i, got[i], tt.want[i])
			}
		}
	}
}

func TestDefaultRoutingConfig(t *testing.T) {
	cfg := DefaultRoutingConfig()
	if cfg.Simple.Lead == "" {
		t.Error("Simple.Lead is empty")
	}
	if cfg.Medium.Lead == "" {
		t.Error("Medium.Lead is empty")
	}
	if cfg.Complex.Lead == "" {
		t.Error("Complex.Lead is empty")
	}
	if cfg.Complex.Fallback == "" {
		t.Error("Complex.Fallback is empty")
	}
}
