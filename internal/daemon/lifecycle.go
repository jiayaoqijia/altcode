package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// activeStates are task statuses that indicate an in-progress task.
// On daemon restart, tasks in these states are considered orphaned.
var activeStates = []string{
	"planning", "implementing", "reviewing", "testing",
	"awaiting_spec", "pr_open",
}

// RecoverOrphanedTasks marks any tasks in active states as failed
// on daemon startup. Returns the number of recovered tasks.
func RecoverOrphanedTasks(store *Store) (int, error) {
	count := 0
	for _, status := range activeStates {
		tasks, err := store.ListTasksByStatus(status)
		if err != nil {
			return count, fmt.Errorf("list %s tasks: %w", status, err)
		}
		for _, t := range tasks {
			if err := store.MarkFailed(t.ID, "daemon restart \u2014 task interrupted"); err != nil {
				return count, fmt.Errorf("mark failed %s: %w", t.ID, err)
			}
			store.AppendEvent(t.ID, "daemon_crash_recovery",
				fmt.Sprintf(`{"previous_status":"%s"}`, status))
			count++
		}
	}
	return count, nil
}

// TaskRunner manages a single task's execution lifecycle.
type TaskRunner struct {
	task     *Task
	store    *Store
	orch     *Orchestrator
	cancel   context.CancelFunc
	logger   *slog.Logger
	timeout  time.Duration
	stopped  atomic.Bool
}

// NewTaskRunner creates a runner for a task.
func NewTaskRunner(task *Task, store *Store, orch *Orchestrator, logger *slog.Logger) *TaskRunner {
	return &TaskRunner{
		task:    task,
		store:   store,
		orch:    orch,
		logger:  logger,
		timeout: 2 * time.Hour,
	}
}

// SetTimeout overrides the default 2-hour timeout.
func (r *TaskRunner) SetTimeout(d time.Duration) {
	r.timeout = d
}

// Run executes the task with timeout and panic recovery.
func (r *TaskRunner) Run(ctx context.Context) {
	ctx, r.cancel = context.WithTimeout(ctx, r.timeout)
	defer r.cancel()

	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("task panicked",
				"task", r.task.ID, "panic", rec)
			r.store.MarkFailed(r.task.ID,
				fmt.Sprintf("panic: %v", rec))
		}
	}()

	r.store.MarkStarted(r.task.ID)

	err := r.orch.RunTask(ctx, r.task)

	// Check stop flag first — user cancellation takes priority.
	if r.stopped.Load() {
		r.store.UpdateStatus(r.task.ID, "cancelled")
		r.store.AppendEvent(r.task.ID, "cancelled_by_user", "")
		return
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			r.store.MarkFailed(r.task.ID,
				"wall-clock timeout exceeded")
			r.store.AppendEvent(r.task.ID, "timeout", "")
		}
		return
	}
}

// Stop cancels the running task. Safe to call before or during Run.
func (r *TaskRunner) Stop() {
	r.stopped.Store(true)
	if r.cancel != nil {
		r.cancel()
	}
}
