package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer builds a Server with an in-memory store and a
// configurable SpawnFunc, bypassing HTTP/filesystem setup.
func newTestServer(
	t *testing.T, maxTasks int, spawn SpawnFunc,
) *Server {
	t.Helper()
	store, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	logger := slog.New(slog.NewTextHandler(
		os.Stderr, &slog.HandlerOptions{Level: slog.LevelError},
	))
	cm := NewConcurrencyManager(maxTasks, logger)
	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc: spawn,
		Logger:    logger,
	})
	return &Server{
		cfg:    ServerConfig{MaxTasks: maxTasks},
		store:  store,
		logger: logger,
		cm:     cm,
		orch:   orch,
	}
}

func TestDispatchTask_RunsToCompletion(t *testing.T) {
	s := newTestServer(t, 2, func(
		_ context.Context, _ AgentConfig,
	) (string, error) {
		return `{"steps":[]}`, nil
	})

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "build it",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	// Run synchronously (dispatchTask blocks until done).
	s.dispatchTask(task)

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want merged", got.Status)
	}
	if got.StartedAt == nil {
		t.Error("started_at should be set")
	}

	// Runner should be cleaned up from the map.
	if _, ok := s.runners.Load(task.ID); ok {
		t.Error("runner should be deleted after completion")
	}

	// Concurrency slot should be released.
	if s.cm.ActiveCount() != 0 {
		t.Errorf("active = %d, want 0", s.cm.ActiveCount())
	}
}

func TestDispatchTask_StopCancels(t *testing.T) {
	started := make(chan struct{})
	s := newTestServer(t, 2, func(
		ctx context.Context, _ AgentConfig,
	) (string, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return "", ctx.Err()
	})

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "slow task",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		s.dispatchTask(task)
		close(done)
	}()

	// Wait for the spawn function to start.
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("spawn did not start")
	}

	// Stop via the runners map, same as handleStopTask.
	v, ok := s.runners.Load(task.ID)
	if !ok {
		t.Fatal("runner not found in map")
	}
	v.(*TaskRunner).Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatchTask did not return after stop")
	}

	got, _ := s.store.GetTask(task.ID)
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", got.Status)
	}

	if s.cm.ActiveCount() != 0 {
		t.Errorf("active = %d, want 0 after cancel",
			s.cm.ActiveCount())
	}
}

func TestDispatchTask_ConcurrencyLimit(t *testing.T) {
	const maxTasks = 2

	// Gate channels: each running task blocks until released.
	gates := make([]chan struct{}, maxTasks+1)
	for i := range gates {
		gates[i] = make(chan struct{})
	}

	var callCount atomic.Int32
	s := newTestServer(t, maxTasks, func(
		ctx context.Context, _ AgentConfig,
	) (string, error) {
		idx := int(callCount.Add(1)) - 1
		if idx < len(gates) {
			select {
			case <-gates[idx]:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return `{"steps":[]}`, nil
	})

	// Create maxTasks+1 tasks.
	tasks := make([]*Task, maxTasks+1)
	for i := range tasks {
		tasks[i] = &Task{
			RepoURL:         "https://github.com/t/r",
			TaskDescription: fmt.Sprintf("task-%d", i),
			Status:          "pending",
		}
		if err := s.store.CreateTask(tasks[i]); err != nil {
			t.Fatal(err)
		}
	}

	// Dispatch all tasks concurrently.
	dones := make([]chan struct{}, len(tasks))
	for i, task := range tasks {
		dones[i] = make(chan struct{})
		go func(task *Task, done chan struct{}) {
			s.dispatchTask(task)
			close(done)
		}(task, dones[i])
	}

	// Wait for the first maxTasks to acquire slots.
	deadline := time.After(5 * time.Second)
	for {
		if s.cm.ActiveCount() >= maxTasks {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("only %d slots acquired, want %d",
				s.cm.ActiveCount(), maxTasks)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// The extra task should have returned immediately (no slot).
	// Give it a moment to finish its dispatchTask call.
	time.Sleep(100 * time.Millisecond)

	// Exactly maxTasks should be active.
	if s.cm.ActiveCount() != maxTasks {
		t.Errorf("active = %d, want %d",
			s.cm.ActiveCount(), maxTasks)
	}

	// Release all gates so active tasks finish.
	for _, g := range gates {
		close(g)
	}

	// Wait for all dispatches to return.
	for i, done := range dones {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("dispatch %d did not finish", i)
		}
	}
}

func TestPollPendingTasks(t *testing.T) {
	spawned := make(chan string, 4)
	s := newTestServer(t, 2, func(
		_ context.Context, _ AgentConfig,
	) (string, error) {
		spawned <- "called"
		return `{"steps":[]}`, nil
	})

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "orphan",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	// Start the poller.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.pollPendingTasks(ctx)

	// The poller should pick up the pending task within a few ticks.
	select {
	case <-spawned:
		// ok -- task was dispatched
	case <-time.After(15 * time.Second):
		t.Fatal("poller did not dispatch pending task")
	}

	// Give the dispatch goroutine time to complete.
	time.Sleep(200 * time.Millisecond)

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want merged", got.Status)
	}
}

func TestDispatchTask_FailedTask(t *testing.T) {
	s := newTestServer(t, 2, func(
		_ context.Context, _ AgentConfig,
	) (string, error) {
		return "", fmt.Errorf("agent crashed")
	})

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "will fail",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	s.dispatchTask(task)

	got, _ := s.store.GetTask(task.ID)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}

	// Slot and runner should be cleaned up even on failure.
	if _, ok := s.runners.Load(task.ID); ok {
		t.Error("runner should be deleted after failure")
	}
	if s.cm.ActiveCount() != 0 {
		t.Errorf("active = %d, want 0", s.cm.ActiveCount())
	}
}

func TestPollPendingTasks_SkipsActiveRunners(t *testing.T) {
	var spawnCount atomic.Int32
	s := newTestServer(t, 2, func(
		ctx context.Context, _ AgentConfig,
	) (string, error) {
		spawnCount.Add(1)
		<-ctx.Done()
		return "", ctx.Err()
	})

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "active",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	// Manually register a runner to simulate an already-dispatched
	// task.
	runner := NewTaskRunner(task, s.store, s.orch, s.logger)
	s.runners.Store(task.ID, runner)
	defer s.runners.Delete(task.ID)

	ctx, cancel := context.WithCancel(context.Background())
	go s.pollPendingTasks(ctx)

	// Wait long enough for at least one poll tick.
	time.Sleep(7 * time.Second)
	cancel()

	// The poller should have skipped this task because a runner
	// is already registered.
	if spawnCount.Load() != 0 {
		t.Errorf("spawn called %d times, want 0 (already active)",
			spawnCount.Load())
	}
}
