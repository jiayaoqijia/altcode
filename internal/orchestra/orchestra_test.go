package orchestra

import (
	"context"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/wfdef"
)

func TestRunWorkflow_SinglePhase(t *testing.T) {
	def := &wfdef.WorkflowDef{
		Name: "test",
		Phases: []wfdef.PhaseDef{{
			Name:      "echo",
			Agents:    []wfdef.AgentAssignment{{Role: "worker", Backend: "echo"}},
			Timeout:   5 * time.Second,
			OnFailure: wfdef.FailureAbort,
		}},
	}

	events := make(chan PhaseEvent, 100)
	override := make(chan OverrideCmd)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := Run(ctx, RunParams{
		Def:      def,
		Task:     "hello",
		WorkDir:  t.TempDir(),
		Events:   events,
		Override: override,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Drain and check for phase_done
	close(events)
	found := false
	for ev := range events {
		if ev.Type == KindPhaseDone {
			found = true
		}
	}
	if !found {
		t.Error("expected KindPhaseDone event")
	}
}

func TestRunWorkflow_DependencySkip(t *testing.T) {
	def := &wfdef.WorkflowDef{
		Name: "dep-test",
		Phases: []wfdef.PhaseDef{
			{
				Name:      "fail-phase",
				Agents:    []wfdef.AgentAssignment{{Role: "worker", Backend: "false"}}, // exits 1
				Timeout:   5 * time.Second,
				OnFailure: wfdef.FailureSkip,
			},
			{
				Name:      "depends-on-fail",
				DependsOn: []string{"fail-phase"},
				Agents:    []wfdef.AgentAssignment{{Role: "worker", Backend: "echo"}},
				Timeout:   5 * time.Second,
				OnFailure: wfdef.FailureAbort,
			},
		},
	}

	events := make(chan PhaseEvent, 100)
	override := make(chan OverrideCmd)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := Run(ctx, RunParams{
		Def:      def,
		Task:     "hello",
		WorkDir:  t.TempDir(),
		Events:   events,
		Override: override,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Second phase should be skipped
	close(events)
	for ev := range events {
		if ev.Phase == "depends-on-fail" && ev.Type == KindPhaseDone {
			if ev.Text != "skipped (dependency failed)" {
				t.Errorf("expected skipped, got %q", ev.Text)
			}
			return
		}
	}
	t.Error("expected skipped event for depends-on-fail phase")
}

func TestRunWorkflow_Abort(t *testing.T) {
	def := &wfdef.WorkflowDef{
		Name: "abort-test",
		Phases: []wfdef.PhaseDef{
			{
				Name:      "slow",
				Agents:    []wfdef.AgentAssignment{{Role: "worker", Backend: "sleep"}},
				Timeout:   30 * time.Second,
				OnFailure: wfdef.FailureAbort,
			},
		},
	}

	events := make(chan PhaseEvent, 100)
	override := make(chan OverrideCmd, 1)
	override <- OverrideCmd{Op: OpAbort} // inject abort before phase starts

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := Run(ctx, RunParams{
		Def:      def,
		Task:     "hello",
		WorkDir:  t.TempDir(),
		Events:   events,
		Override: override,
	})
	if err == nil {
		t.Error("expected abort error")
	}
}

func TestVerdictString(t *testing.T) {
	tests := []struct {
		v    Verdict
		want string
	}{
		{VerdictPass, "pass"},
		{VerdictFail, "fail"},
		{VerdictTimeout, "timeout"},
		{VerdictSkipped, "skipped"},
	}
	for _, tt := range tests {
		if got := tt.v.String(); got != tt.want {
			t.Errorf("Verdict(%d).String() = %q, want %q", tt.v, got, tt.want)
		}
	}
}
