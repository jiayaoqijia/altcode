package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSteerDuringExecution(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "build feature",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	// Track prompts received by the implementer.
	var mu sync.Mutex
	var implPrompts []string

	steerCh := make(chan string, 10)
	planDone := make(chan struct{})

	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				// Signal plan completion so test can inject steer
				// before the implement drain runs.
				defer close(planDone)
				return `{"steps":[{"description":"s1","prompt":"do step 1"}]}`, nil
			}
			if cfg.Role == "implementer" {
				prompt := cfg.Args[len(cfg.Args)-1]
				mu.Lock()
				implPrompts = append(implPrompts, prompt)
				mu.Unlock()
				return "ok", nil
			}
			return "ok", nil
		},
	})

	done := make(chan struct{})
	go func() {
		_ = orch.RunTask(context.Background(), task, steerCh)
		close(done)
	}()

	// Wait for plan to finish, then inject steer before the
	// implement loop drains. The plan spawn signals via planDone
	// but the orchestrator still runs emitPhase + emitSpec + status
	// updates before the drain, giving us a safe window.
	<-planDone
	steerCh <- "focus on error handling"

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunTask did not complete")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(implPrompts) != 1 {
		t.Fatalf("expected 1 implementer call, got %d", len(implPrompts))
	}
	if !strings.Contains(implPrompts[0], "User guidance:") {
		t.Errorf("prompt should contain steer prefix, got %q",
			implPrompts[0])
	}
	if !strings.Contains(implPrompts[0], "focus on error handling") {
		t.Errorf("prompt should contain steer message, got %q",
			implPrompts[0])
	}
	// Iteration-2 sanitisation wraps steer + plan step in boundary
	// blocks. Assertions now check both are present — the exact
	// "Original task:" prefix was dropped when we moved to structured
	// wrappers.
	if !strings.Contains(implPrompts[0], "do step 1") {
		t.Errorf("prompt should preserve step text, got %q",
			implPrompts[0])
	}
	if !strings.Contains(implPrompts[0], "USER_STEER") ||
		!strings.Contains(implPrompts[0], "PLAN_STEP") {
		t.Errorf("prompt missing boundary wrappers: %q", implPrompts[0])
	}

	// Verify steer_applied event was recorded.
	events, err := store.ListEvents(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.EventType == "steer_applied" {
			found = true
			if !strings.Contains(e.Data, "focus on error handling") {
				t.Errorf("steer_applied data = %q, want steer message",
					e.Data)
			}
		}
	}
	if !found {
		t.Error("expected steer_applied event")
	}
}

func TestSteerChannelFull(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "overflow test",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, _ AgentConfig) (string, error) {
			return `{"steps":[]}`, nil
		},
	})

	runner := NewTaskRunner(task, store, orch, testLogger())

	// Fill the buffer (capacity 10).
	for i := 0; i < 10; i++ {
		runner.Steer("msg")
	}

	// 11th message should be dropped, not panic or block.
	runner.Steer("overflow")

	// Verify buffer has exactly 10 messages.
	count := 0
	for {
		select {
		case <-runner.steerCh:
			count++
		default:
			goto done
		}
	}
done:
	if count != 10 {
		t.Errorf("drained %d messages, want 10", count)
	}
}

func TestSteerAfterCompletion(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "already done",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, _ AgentConfig) (string, error) {
			return `{"steps":[]}`, nil
		},
	})

	runner := NewTaskRunner(task, store, orch, testLogger())
	runner.Run(context.Background())

	// Task is now complete. Steer should not panic.
	runner.Steer("too late")

	// Message lands in channel (not consumed), no crash.
	select {
	case msg := <-runner.steerCh:
		if msg != "too late" {
			t.Errorf("unexpected message: %q", msg)
		}
	default:
		t.Error("steer message should be in channel")
	}
}

func TestSteerNilChannel(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "nil steer",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				return `{"steps":[{"description":"s1","prompt":"do it"}]}`, nil
			}
			return "ok", nil
		},
	})

	// Passing nil steerCh should not panic.
	err = orch.RunTask(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("RunTask with nil steerCh should succeed: %v", err)
	}

	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want merged", got.Status)
	}
}

func TestDrainSteerWaitsBrieflyForPostPlanGuidance(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	orch := NewOrchestrator(store, OrchestratorConfig{})
	steerCh := make(chan string, 1)
	go func() {
		time.Sleep(5 * time.Millisecond)
		steerCh <- "focus on tests"
	}()

	got := orch.drainSteer(steerCh)
	if !strings.Contains(got, "focus on tests") {
		t.Fatalf("drainSteer missed near-boundary steer message: %q", got)
	}
}

func TestSteerMultipleMessages(t *testing.T) {
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "multi steer",
		Status:          "pending",
	}
	if err := store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var implPrompts []string

	steerCh := make(chan string, 10)
	planDone := make(chan struct{})

	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				defer close(planDone)
				return `{"steps":[{"description":"s1","prompt":"do it"}]}`, nil
			}
			if cfg.Role == "implementer" {
				prompt := cfg.Args[len(cfg.Args)-1]
				mu.Lock()
				implPrompts = append(implPrompts, prompt)
				mu.Unlock()
				return "ok", nil
			}
			return "ok", nil
		},
	})

	done := make(chan struct{})
	go func() {
		_ = orch.RunTask(context.Background(), task, steerCh)
		close(done)
	}()

	// Wait for plan, then send multiple steer messages before drain.
	<-planDone
	steerCh <- "use Go 1.22"
	steerCh <- "add tests"

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunTask did not complete")
	}

	mu.Lock()
	defer mu.Unlock()

	if len(implPrompts) != 1 {
		t.Fatalf("expected 1 implementer call, got %d", len(implPrompts))
	}
	// Both messages should be joined with "; ".
	if !strings.Contains(implPrompts[0], "use Go 1.22") {
		t.Errorf("prompt missing first steer, got %q", implPrompts[0])
	}
	if !strings.Contains(implPrompts[0], "add tests") {
		t.Errorf("prompt missing second steer, got %q", implPrompts[0])
	}
	if !strings.Contains(implPrompts[0], "; ") {
		t.Errorf("multiple steers should be joined with '; ', got %q",
			implPrompts[0])
	}
}
