package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOrchestrator_PhasesTransition(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "fix auth bug",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	var calls []string
	o := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			calls = append(calls, cfg.Role)
			if cfg.Role == "lead" {
				return `{"steps":[{"description":"add auth","prompt":"implement auth"}]}`, nil
			}
			return `{"verdict":"pass"}`, nil
		},
	})

	if err := o.RunTask(context.Background(), task); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	// Verify task ended as merged (via MarkCompleted).
	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("final status = %q, want %q", got.Status, "merged")
	}

	// Verify StartedAt was set.
	if got.StartedAt == nil {
		t.Error("StartedAt not set")
	}

	// Verify CompletedAt was set.
	if got.CompletedAt == nil {
		t.Error("CompletedAt not set")
	}

	// Verify agent roles were called in order.
	wantRoles := []string{"lead", "implementer", "reviewer"}
	if len(calls) != len(wantRoles) {
		t.Fatalf("calls = %v, want %v", calls, wantRoles)
	}
	for i, want := range wantRoles {
		if calls[i] != want {
			t.Errorf("call[%d] = %q, want %q", i, calls[i], want)
		}
	}

	// Verify events were logged (emitPhase -> AppendEvent).
	events, err := store.ListEvents(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// plan started/completed, spec, implement started/completed,
	// review started/completed, finalize started/completed = 9
	if len(events) < 9 {
		t.Errorf("expected at least 9 events (8 phase + 1 spec), got %d",
			len(events))
	}

	// Verify event types contain expected phases.
	var types []string
	for _, e := range events {
		types = append(types, e.EventType)
	}
	for _, want := range []string{"phase_started", "phase_completed"} {
		found := false
		for _, got := range types {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing event type %q in %v", want, types)
		}
	}

	// Verify event data contains phase names.
	var phases []string
	for _, e := range events {
		var d map[string]string
		if err := json.Unmarshal([]byte(e.Data), &d); err == nil {
			phases = append(phases, d["phase"])
		}
	}
	for _, want := range []string{"plan", "implement", "review", "finalize"} {
		found := false
		for _, p := range phases {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing phase %q in events, got %v", want, phases)
		}
	}
}

func TestOrchestrator_RetryOnFailure(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "flaky step",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	// Implementer fails on first call, succeeds on second.
	var implAttempts int32
	o := NewOrchestrator(store, OrchestratorConfig{
		MaxFixRetry: 3,
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				return `{"steps":[{"description":"s1","prompt":"do it"}]}`, nil
			}
			if cfg.Role == "implementer" {
				n := atomic.AddInt32(&implAttempts, 1)
				if n == 1 {
					return "", fmt.Errorf("transient error")
				}
				return "ok", nil
			}
			return "ok", nil
		},
	})

	if err := o.RunTask(context.Background(), task); err != nil {
		t.Fatalf("RunTask should succeed after retry: %v", err)
	}

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want %q", got.Status, "merged")
	}

	if atomic.LoadInt32(&implAttempts) != 2 {
		t.Errorf("implementer attempts = %d, want 2", implAttempts)
	}
}

func TestOrchestrator_AllAttemptsExhausted(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "always fails",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	var implAttempts int32
	o := NewOrchestrator(store, OrchestratorConfig{
		MaxFixRetry: 2,
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				return `{"steps":[{"description":"s1","prompt":"do it"}]}`, nil
			}
			if cfg.Role == "implementer" {
				atomic.AddInt32(&implAttempts, 1)
				return "", fmt.Errorf("permanent error")
			}
			return "ok", nil
		},
	})

	err = o.RunTask(context.Background(), task)
	if err == nil {
		t.Fatal("RunTask should fail when all attempts exhausted")
	}
	if !strings.Contains(err.Error(), "failed after 2 attempts") {
		t.Errorf("error = %q, want mention of exhausted attempts", err)
	}

	got, gErr := store.GetTask(task.ID)
	if gErr != nil {
		t.Fatal(gErr)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want %q", got.Status, "failed")
	}
	if got.ErrorMessage == "" {
		t.Error("ErrorMessage should be set on failure")
	}
	if !strings.Contains(got.ErrorMessage, "permanent error") {
		t.Errorf("ErrorMessage = %q, want mention of permanent error",
			got.ErrorMessage)
	}

	if atomic.LoadInt32(&implAttempts) != 2 {
		t.Errorf("implementer attempts = %d, want 2", implAttempts)
	}
}

func TestOrchestrator_MalformedPlanJSON(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "handle garbage plan",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	var implCalls []string
	o := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				// Return garbage that is not valid JSON.
				return "this is not json {{{", nil
			}
			if cfg.Role == "implementer" {
				implCalls = append(implCalls, strings.Join(cfg.Args, " "))
				return "ok", nil
			}
			return "ok", nil
		},
	})

	if err := o.RunTask(context.Background(), task); err != nil {
		t.Fatalf("RunTask should succeed with fallback plan: %v", err)
	}

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want %q", got.Status, "merged")
	}

	// Fallback plan uses task description as the single step prompt.
	if len(implCalls) != 1 {
		t.Fatalf("implementer calls = %d, want 1 (fallback single step)",
			len(implCalls))
	}
	if implCalls[0] != task.TaskDescription {
		t.Errorf("fallback prompt = %q, want %q",
			implCalls[0], task.TaskDescription)
	}
}

func TestOrchestrator_ContextCancellation(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "cancel me",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	o := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(c context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				return `{"steps":[{"description":"s1","prompt":"do"}]}`, nil
			}
			// Cancel context when implementer is called.
			cancel()
			return "", c.Err()
		},
	})

	err = o.RunTask(ctx, task)
	if err == nil {
		t.Fatal("RunTask should fail on cancelled context")
	}

	got, gErr := store.GetTask(task.ID)
	if gErr != nil {
		t.Fatal(gErr)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want %q", got.Status, "failed")
	}
}

func TestOrchestrator_EmptyPlan(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "empty plan",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	var roles []string
	o := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			roles = append(roles, cfg.Role)
			if cfg.Role == "lead" {
				// Valid JSON but empty steps array.
				return `{"steps":[]}`, nil
			}
			return "ok", nil
		},
	})

	if err := o.RunTask(context.Background(), task); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want %q", got.Status, "merged")
	}

	// Empty steps triggers fallback single-step plan, so
	// implementer should still be called once.
	implCount := 0
	for _, r := range roles {
		if r == "implementer" {
			implCount++
		}
	}
	if implCount != 1 {
		t.Errorf("implementer calls = %d, want 1 (fallback)", implCount)
	}
}

func TestOrchestrator_ValidJSONNoSteps(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "json no steps",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	var roles []string
	o := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			roles = append(roles, cfg.Role)
			if cfg.Role == "lead" {
				// Valid JSON object but missing "steps" key entirely.
				return `{"analysis":"looks good"}`, nil
			}
			return "ok", nil
		},
	})

	if err := o.RunTask(context.Background(), task); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want %q", got.Status, "merged")
	}

	// Missing steps field => Plan.Steps is nil (len 0) => fallback.
	implCount := 0
	for _, r := range roles {
		if r == "implementer" {
			implCount++
		}
	}
	if implCount != 1 {
		t.Errorf("implementer calls = %d, want 1 (fallback)", implCount)
	}
}

func TestOrchestrator_SpecEventEmitted(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "add feature",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	o := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				return `{"steps":[
					{"description":"add auth","prompt":"do auth"},
					{"description":"add tests","prompt":"do tests"}
				]}`, nil
			}
			return "ok", nil
		},
	})

	if err := o.RunTask(context.Background(), task); err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	events, err := store.ListEvents(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	var specEvent *TaskEvent
	for _, e := range events {
		if e.EventType == "spec" {
			specEvent = e
			break
		}
	}
	if specEvent == nil {
		t.Fatal("expected spec event after plan phase")
	}

	var spec map[string]any
	if err := json.Unmarshal([]byte(specEvent.Data), &spec); err != nil {
		t.Fatalf("unmarshal spec data: %v", err)
	}
	targets, ok := spec["target_state"].([]any)
	if !ok {
		t.Fatalf("expected target_state array, got: %v", spec)
	}
	if len(targets) != 2 {
		t.Errorf("expected 2 target states, got %d", len(targets))
	}
	if targets[0] != "add auth" {
		t.Errorf("target[0] = %v, want 'add auth'", targets[0])
	}
}

func TestExtractTargetState(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{Description: "step one"},
		{Description: "step two"},
		{Description: "step three"},
	}}
	targets := extractTargetState(plan)
	if len(targets) != 3 {
		t.Fatalf("got %d targets, want 3", len(targets))
	}
	if targets[0] != "step one" {
		t.Errorf("targets[0] = %q", targets[0])
	}
	if targets[2] != "step three" {
		t.Errorf("targets[2] = %q", targets[2])
	}

	// Nil plan.
	empty := extractTargetState(nil)
	if len(empty) != 0 {
		t.Errorf("nil plan should return empty slice, got %d", len(empty))
	}

	// Empty steps.
	empty2 := extractTargetState(&Plan{})
	if len(empty2) != 0 {
		t.Errorf("empty steps should return empty slice, got %d",
			len(empty2))
	}
}
