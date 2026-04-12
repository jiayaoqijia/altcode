package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRecoverOrphanedTasks(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Create tasks in various states
	t1 := &Task{RepoURL: "r", TaskDescription: "a", Status: "implementing"}
	t2 := &Task{RepoURL: "r", TaskDescription: "b", Status: "pending"}
	t3 := &Task{RepoURL: "r", TaskDescription: "c", Status: "reviewing"}
	t4 := &Task{RepoURL: "r", TaskDescription: "d", Status: "completed"}
	for _, task := range []*Task{t1, t2, t3, t4} {
		if err := s.CreateTask(task); err != nil {
			t.Fatalf("CreateTask: %v", err)
		}
	}

	count, err := RecoverOrphanedTasks(s)
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks: %v", err)
	}
	// Only implementing + reviewing should be recovered
	if count != 2 {
		t.Errorf("recovered %d tasks, want 2", count)
	}

	got1, _ := s.GetTask(t1.ID)
	if got1.Status != "failed" {
		t.Errorf("t1 status = %q, want failed", got1.Status)
	}
	if got1.ErrorMessage != "daemon restart \u2014 task interrupted" {
		t.Errorf("t1 error = %q", got1.ErrorMessage)
	}

	// pending and completed should be unchanged
	got2, _ := s.GetTask(t2.ID)
	if got2.Status != "pending" {
		t.Errorf("t2 should stay pending, got %q", got2.Status)
	}
	got4, _ := s.GetTask(t4.ID)
	if got4.Status != "completed" {
		t.Errorf("t4 should stay completed, got %q", got4.Status)
	}
}

func TestRecoverOrphanedTasks_AllActiveStates(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Create one task for each active state
	for _, status := range activeStates {
		task := &Task{
			RepoURL:         "r",
			TaskDescription: status,
			Status:          status,
		}
		if err := s.CreateTask(task); err != nil {
			t.Fatalf("CreateTask(%s): %v", status, err)
		}
	}

	count, err := RecoverOrphanedTasks(s)
	if err != nil {
		t.Fatalf("RecoverOrphanedTasks: %v", err)
	}
	if count != len(activeStates) {
		t.Errorf("recovered %d, want %d", count, len(activeStates))
	}
}

func TestRecoverOrphanedTasks_EmptyStore(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	count, err := RecoverOrphanedTasks(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("recovered %d tasks from empty store, want 0", count)
	}
}

func TestRecoverOrphanedTasks_Idempotent(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "r",
		TaskDescription: "x",
		Status:          "planning",
	}
	s.CreateTask(task)

	// First recovery
	count1, err := RecoverOrphanedTasks(s)
	if err != nil {
		t.Fatal(err)
	}
	if count1 != 1 {
		t.Errorf("first pass recovered %d, want 1", count1)
	}

	// Second recovery should find nothing (already failed)
	count2, err := RecoverOrphanedTasks(s)
	if err != nil {
		t.Fatal(err)
	}
	if count2 != 0 {
		t.Errorf("second pass recovered %d, want 0", count2)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func TestTaskRunner_HappyPath(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "r",
		TaskDescription: "build it",
		Status:          "pending",
	}
	s.CreateTask(task)

	orch := NewOrchestrator(s, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, _ AgentConfig) (string, error) {
			return `{"steps":[]}`, nil
		},
	})

	runner := NewTaskRunner(task, s, orch, testLogger())
	runner.Run(context.Background())

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want merged", got.Status)
	}
	if got.StartedAt == nil {
		t.Error("started_at should be set")
	}
	if got.CompletedAt == nil {
		t.Error("completed_at should be set")
	}
}

func TestTaskRunner_Timeout(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "r",
		TaskDescription: "slow",
		Status:          "pending",
	}
	s.CreateTask(task)

	orch := NewOrchestrator(s, OrchestratorConfig{
		SpawnFunc: func(ctx context.Context, _ AgentConfig) (string, error) {
			// Block until context cancels (simulates a hanging agent)
			<-ctx.Done()
			return "", ctx.Err()
		},
	})

	runner := NewTaskRunner(task, s, orch, testLogger())
	runner.SetTimeout(50 * time.Millisecond)
	runner.Run(context.Background())

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.ErrorMessage, "timeout") {
		t.Errorf("error = %q, want timeout mention", got.ErrorMessage)
	}
}

func TestTaskRunner_PanicRecovery(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "r",
		TaskDescription: "crashy",
		Status:          "pending",
	}
	s.CreateTask(task)

	orch := NewOrchestrator(s, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, _ AgentConfig) (string, error) {
			panic("segfault simulation")
		},
	})

	runner := NewTaskRunner(task, s, orch, testLogger())
	// Should not panic the caller
	runner.Run(context.Background())

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.ErrorMessage, "panic") {
		t.Errorf("error = %q, want panic mention", got.ErrorMessage)
	}
	if !strings.Contains(got.ErrorMessage, "segfault simulation") {
		t.Errorf("error = %q, want panic value", got.ErrorMessage)
	}
}

func TestTaskRunner_Stop(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "r",
		TaskDescription: "cancellable",
		Status:          "pending",
	}
	s.CreateTask(task)

	started := make(chan struct{})
	orch := NewOrchestrator(s, OrchestratorConfig{
		SpawnFunc: func(ctx context.Context, _ AgentConfig) (string, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		},
	})

	runner := NewTaskRunner(task, s, orch, testLogger())
	runner.SetTimeout(10 * time.Second)

	done := make(chan struct{})
	go func() {
		runner.Run(context.Background())
		close(done)
	}()

	<-started
	runner.Stop()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop")
	}

	got, _ := s.GetTask(task.ID)
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
}

func TestTaskRunner_ContextCancelledExternally(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "r",
		TaskDescription: "external cancel",
		Status:          "pending",
	}
	s.CreateTask(task)

	orch := NewOrchestrator(s, OrchestratorConfig{
		SpawnFunc: func(ctx context.Context, _ AgentConfig) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	runner := NewTaskRunner(task, s, orch, testLogger())
	runner.SetTimeout(10 * time.Second)

	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	// Give it a moment to start, then cancel externally
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after external cancel")
	}
}

func TestTaskRunner_OrchestratorError(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "r",
		TaskDescription: "broken",
		Status:          "pending",
	}
	s.CreateTask(task)

	callCount := 0
	orch := NewOrchestrator(s, OrchestratorConfig{
		MaxFixRetry: 1,
		SpawnFunc: func(_ context.Context, _ AgentConfig) (string, error) {
			callCount++
			return "", fmt.Errorf("agent crashed")
		},
	})

	runner := NewTaskRunner(task, s, orch, testLogger())
	runner.Run(context.Background())

	got, _ := s.GetTask(task.ID)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
}
