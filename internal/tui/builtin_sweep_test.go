package tui

import (
	"strings"
	"testing"
)

// TestBuiltinTextProducers_NoPanicOnNilEngine sweeps every builtin*Text
// helper that takes no arguments. The contract: with a freshly-built
// App and a nil engine, the function must return a string (possibly
// containing a fallback message) without panicking. This is the most
// common path users hit before any model has been called — failures
// here have shipped twice in the past quarter.
func TestBuiltinTextProducers_NoPanicOnNilEngine(t *testing.T) {
	a := testApp() // engine is nil here

	cases := []struct {
		name string
		fn   func() string
	}{
		{"status", a.builtinStatusText},
		{"context", a.builtinContextText},
		{"model", a.builtinModelText},
		{"tools", a.builtinToolsText},
		{"sessions", a.builtinSessionsText},
		{"cost", a.builtinCostText},
		{"history", a.builtinHistoryText},
		{"compact", a.builtinCompactText},
		{"diff", a.builtinDiffText},
		{"plan", a.builtinPlanText},
		{"stats", a.builtinStatsText},
		{"costSummary", a.builtinCostSummary},
		{"fileSummary", a.builtinFileSummary},
		{"version", a.builtinVersionText},
		{"tasks", a.builtinTasksText},
		{"workflowStatus", a.builtinWorkflowStatusText},
		{"workflowCancel", a.builtinWorkflowCancelText},
		{"workflowPause", a.builtinWorkflowPauseText},
		{"workflowResume", a.builtinWorkflowResumeText},
		{"workspaceList", a.builtinWorkspaceListText},
		{"workspaceStatus", a.builtinWorkspaceStatusText},
		{"agents", a.builtinAgentsText},
		{"skills", a.builtinSkillsText},
		{"mcp", a.builtinMCPText},
		{"plugins", a.builtinPluginsText},
		{"team", a.builtinTeamText},
		{"backends", a.builtinBackendsText},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s() panicked: %v", c.name, r)
				}
			}()
			out := c.fn()
			if out == "" {
				t.Errorf("%s() returned empty string", c.name)
			}
		})
	}
}

// TestBuiltinHelpText covers the static help producer. It must list
// every shipped slash command (registry coverage) so users discover
// features without reading the source.
func TestBuiltinHelpText(t *testing.T) {
	out := builtinHelpText()
	for _, want := range []string{"/help", "/clear", "/diff", "/model", "/tools", "/sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("help text missing %q", want)
		}
	}
}

// TestBuiltinSearchText_EmptyAndPattern covers the search builtin under
// nil-engine conditions: empty query → guidance message, real query →
// non-empty result (even when no hits).
func TestBuiltinSearchText_EmptyAndPattern(t *testing.T) {
	a := testApp()

	if got := a.builtinSearchText(""); got == "" {
		t.Error("empty-query search returned empty string")
	}
	if got := a.builtinSearchText("nonexistent_xyzpattern_42"); got == "" {
		t.Error("real-query search returned empty string")
	}
}

// TestBuiltinMemoryText_VariousParts walks the parts switch in the
// memory subcommand: bare /memory, /memory list, /memory show, etc.
// All paths must return non-empty without panic on a nil engine.
func TestBuiltinMemoryText_VariousParts(t *testing.T) {
	a := testApp()
	cases := [][]string{
		{"/memory"},
		{"/memory", "list"},
		{"/memory", "show"},
		{"/memory", "clear"},
		{"/memory", "show", "missing.md"},
	}
	for _, parts := range cases {
		got := a.builtinMemoryText(parts)
		if got == "" {
			t.Errorf("memory %v returned empty string", parts)
		}
	}
}

// TestBuiltinRenameText_UpdatesDisplayName covers the in-memory
// session label used by the HUD.
func TestBuiltinRenameText_UpdatesDisplayName(t *testing.T) {
	a := testApp()
	a.sessionSlug = "default-session"

	got := a.builtinRenameText([]string{"/rename", "Release", "notes"})
	if !strings.Contains(got, "Release notes") {
		t.Fatalf("rename output missing new title: %q", got)
	}
	if a.sessionDisplayName() != "Release notes" {
		t.Fatalf("sessionDisplayName() = %q, want %q", a.sessionDisplayName(), "Release notes")
	}
}
