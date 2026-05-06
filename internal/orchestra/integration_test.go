package orchestra

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/wfdef"
)

func TestIntegration_TwoPhaseWorkflow(t *testing.T) {
	// Parse a workflow definition from YAML
	yaml := `---
name: integration-test
description: test two-phase workflow
phases:
  - name: phase1
    agents:
      - role: worker1
        backend: echo
    timeout: 5s
  - name: phase2
    depends_on: [phase1]
    agents:
      - role: worker2
        backend: echo
    timeout: 5s
---
Integration test.
`
	def, err := wfdef.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	events := make(chan PhaseEvent, 200)
	override := make(chan OverrideCmd)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err = Run(ctx, RunParams{
		Def:      def,
		Task:     "integration test task",
		WorkDir:  t.TempDir(),
		Events:   events,
		Override: override,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Collect all phase_done events
	close(events)
	donePhases := map[string]string{}
	for ev := range events {
		if ev.Type == KindPhaseDone {
			donePhases[ev.Phase] = ev.Text
		}
	}

	// Both phases should complete
	if _, ok := donePhases["phase1"]; !ok {
		t.Error("phase1 did not complete")
	}
	if _, ok := donePhases["phase2"]; !ok {
		t.Error("phase2 did not complete")
	}
}

func TestIntegration_ParallelPhase(t *testing.T) {
	yaml := `---
name: parallel-test
phases:
  - name: review
    parallel: true
    agents:
      - role: reviewer-a
        backend: echo
      - role: reviewer-b
        backend: echo
    timeout: 5s
---
Parallel test.
`
	def, err := wfdef.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	events := make(chan PhaseEvent, 200)
	override := make(chan OverrideCmd)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = Run(ctx, RunParams{
		Def:      def,
		Task:     "parallel review task",
		WorkDir:  t.TempDir(),
		Events:   events,
		Override: override,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should get events from both roles
	close(events)
	roles := map[string]bool{}
	for ev := range events {
		if ev.Role != "" {
			roles[ev.Role] = true
		}
	}
	if !roles["reviewer-a"] || !roles["reviewer-b"] {
		t.Errorf("expected both reviewers, got %v", roles)
	}
}

func TestIntegration_SkipOverride(t *testing.T) {
	yaml := `---
name: skip-test
phases:
  - name: slow
    agents:
      - role: worker
        backend: sleep
    timeout: 30s
---
Skip test.
`
	def, err := wfdef.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	events := make(chan PhaseEvent, 200)
	override := make(chan OverrideCmd, 1)
	// Pre-inject skip command
	override <- OverrideCmd{Op: OpSkip}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = Run(ctx, RunParams{
		Def:      def,
		Task:     "should be skipped",
		WorkDir:  t.TempDir(),
		Events:   events,
		Override: override,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	close(events)
	for ev := range events {
		if ev.Type == KindPhaseDone && ev.Text == "skipped" {
			return // success
		}
	}
	t.Error("expected skipped phase_done event")
}

func TestIntegration_WorkflowDiscover(t *testing.T) {
	// Resolve .altcode/workflows/ relative to the package directory so the
	// test works in any checkout location (local dev and CI both).
	workflowDir := filepath.Join("..", "..", ".altcode", "workflows")
	if _, err := os.Stat(workflowDir); err != nil {
		t.Skipf("workflow dir %s not available: %v", workflowDir, err)
	}
	defs, err := wfdef.Discover(workflowDir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(defs) < 3 {
		t.Errorf("expected at least 3 workflows (ship-feature, review, fix), got %d", len(defs))
	}

	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
	}
	for _, want := range []string{"ship-feature", "review", "fix"} {
		if !names[want] {
			t.Errorf("missing workflow %q", want)
		}
	}
}
