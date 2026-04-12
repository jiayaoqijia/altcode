package daemon

import (
	"context"
	"log/slog"
	"sync"
)

// ConcurrencyManager controls how many tasks run simultaneously.
type ConcurrencyManager struct {
	sem      chan struct{}
	mu       sync.Mutex
	active   map[string]context.CancelFunc
	maxTasks int
	logger   *slog.Logger
}

// NewConcurrencyManager creates a manager that allows at most
// maxTasks tasks to run concurrently.
func NewConcurrencyManager(
	maxTasks int, logger *slog.Logger,
) *ConcurrencyManager {
	return &ConcurrencyManager{
		sem:      make(chan struct{}, maxTasks),
		active:   make(map[string]context.CancelFunc),
		maxTasks: maxTasks,
		logger:   logger,
	}
}

// TryAcquire attempts to acquire a concurrency slot for taskID.
// Returns true if the slot was acquired, false if all slots are
// full (the caller should queue the task).
func (cm *ConcurrencyManager) TryAcquire(
	taskID string, cancel context.CancelFunc,
) bool {
	select {
	case cm.sem <- struct{}{}:
		cm.mu.Lock()
		cm.active[taskID] = cancel
		cm.mu.Unlock()
		cm.logger.Info("slot acquired",
			"task", taskID, "active", cm.ActiveCount())
		return true
	default:
		return false
	}
}

// Release frees the concurrency slot held by taskID. It is
// idempotent: calling Release on an already-released or unknown
// taskID is a no-op (no double-drain of the semaphore).
func (cm *ConcurrencyManager) Release(taskID string) {
	cm.mu.Lock()
	_, ok := cm.active[taskID]
	if ok {
		delete(cm.active, taskID)
		<-cm.sem
	}
	cm.mu.Unlock()
}

// Cancel cancels a specific running task. If the task is not
// active, Cancel is a no-op.
func (cm *ConcurrencyManager) Cancel(taskID string) {
	cm.mu.Lock()
	if cancel, ok := cm.active[taskID]; ok {
		cancel()
		delete(cm.active, taskID)
		<-cm.sem
	}
	cm.mu.Unlock()
}

// ActiveCount returns the number of currently running tasks.
func (cm *ConcurrencyManager) ActiveCount() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return len(cm.active)
}

// QueuePosition returns how many tasks would be queued ahead
// of a new submission. Returns 0 when slots are available.
func (cm *ConcurrencyManager) QueuePosition() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	overflow := len(cm.active) - cm.maxTasks
	if overflow < 0 {
		return 0
	}
	return overflow
}
