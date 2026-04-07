package wfdef

import (
	"testing"
	"time"
)

const testWorkflow = `---
name: test-flow
description: A test workflow
phases:
  - name: plan
    agents:
      - role: planner
        backend: claude
        model: claude-sonnet-4-20250514
        prompt: "Plan: {{.Task}}"
    timeout: 5m
    required: true
  - name: implement
    depends_on: [plan]
    agents:
      - role: coder
        backend: codex
    timeout: 10m
  - name: review
    depends_on: [implement]
    parallel: true
    on_failure: human
    agents:
      - role: reviewer
        backend: claude
      - role: challenger
        backend: codex
---
Default test workflow.
`

func TestParseWorkflow(t *testing.T) {
	def, err := Parse([]byte(testWorkflow))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if def.Name != "test-flow" {
		t.Errorf("Name = %q, want %q", def.Name, "test-flow")
	}
	if len(def.Phases) != 3 {
		t.Fatalf("Phases = %d, want 3", len(def.Phases))
	}

	p := def.Phases[0]
	if p.Name != "plan" || !p.Required || p.Timeout != 5*time.Minute {
		t.Errorf("phase 0: name=%q required=%v timeout=%v", p.Name, p.Required, p.Timeout)
	}
	if len(p.Agents) != 1 || p.Agents[0].Role != "planner" || p.Agents[0].Backend != "claude" {
		t.Errorf("phase 0 agents: %+v", p.Agents)
	}

	if def.Phases[1].DependsOn[0] != "plan" {
		t.Errorf("phase 1 depends_on = %v", def.Phases[1].DependsOn)
	}

	if !def.Phases[2].Parallel || def.Phases[2].OnFailure != FailureHuman {
		t.Errorf("phase 2: parallel=%v on_failure=%v", def.Phases[2].Parallel, def.Phases[2].OnFailure)
	}
	if len(def.Phases[2].Agents) != 2 {
		t.Errorf("phase 2 agents = %d, want 2", len(def.Phases[2].Agents))
	}
}

func TestParseWorkflow_Invalid(t *testing.T) {
	_, err := Parse([]byte("not yaml at all"))
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestParseWorkflow_NoName(t *testing.T) {
	_, err := Parse([]byte("---\ndescription: no name\nphases: []\n---\n"))
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestTopoSort(t *testing.T) {
	def, _ := Parse([]byte(testWorkflow))
	order, err := def.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	idx := map[string]int{}
	for i, name := range order {
		idx[name] = i
	}
	if idx["plan"] >= idx["implement"] || idx["implement"] >= idx["review"] {
		t.Errorf("bad order: %v", order)
	}
}

func TestTopoSort_Cycle(t *testing.T) {
	def := &WorkflowDef{
		Name: "cycle",
		Phases: []PhaseDef{
			{Name: "a", DependsOn: []string{"b"}},
			{Name: "b", DependsOn: []string{"a"}},
		},
	}
	_, err := def.TopoSort()
	if err == nil {
		t.Error("expected cycle error")
	}
}

func TestPhaseByName(t *testing.T) {
	def, _ := Parse([]byte(testWorkflow))
	p := def.PhaseByName("implement")
	if p == nil || p.Name != "implement" {
		t.Error("PhaseByName failed")
	}
	if def.PhaseByName("nonexistent") != nil {
		t.Error("expected nil for nonexistent phase")
	}
}

func TestDefaultOnFailure(t *testing.T) {
	def, _ := Parse([]byte(testWorkflow))
	// Phase without explicit on_failure should default to abort
	if def.Phases[0].OnFailure != FailureAbort {
		t.Errorf("default on_failure = %q, want abort", def.Phases[0].OnFailure)
	}
}
