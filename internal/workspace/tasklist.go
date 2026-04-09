package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Task represents a single unit of work in a shared workspace task list.
type Task struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"` // "pending", "in_progress", "completed", "blocked"
	Assignee  string    `json:"assignee,omitempty"`
	DependsOn []string  `json:"depends_on,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Output    string    `json:"output,omitempty"`
}

// TaskList is a concurrency-safe, file-backed list of tasks with dependency tracking.
// Stored at .altcode/workspace/{id}/tasks.json.
type TaskList struct {
	mu    sync.Mutex
	path  string
	tasks []*Task
}

// NewTaskList creates a TaskList backed by the given file path.
func NewTaskList(path string) *TaskList {
	return &TaskList{path: path}
}

// Add appends a task and persists the list to disk.
func (tl *TaskList) Add(task *Task) error {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	for _, t := range tl.tasks {
		if t.ID == task.ID {
			return fmt.Errorf("duplicate task ID: %s", task.ID)
		}
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	tl.tasks = append(tl.tasks, task)
	return tl.saveLocked()
}

// Claim finds the next unblocked, unassigned pending task and assigns it
// to the given role. Uses flock to prevent double-claiming across processes.
func (tl *TaskList) Claim(role string) (*Task, error) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	var claimedID string
	if err := tl.flockDo(func() error {
		// Re-read from disk under flock to get latest state.
		if err := tl.loadLocked(); err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, t := range tl.tasks {
			if t.Status != "pending" || t.Assignee != "" {
				continue
			}
			if tl.isBlockedLocked(t.ID) {
				continue
			}
			t.Status = "in_progress"
			t.Assignee = role
			claimedID = t.ID
			return tl.saveLocked()
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if claimedID == "" {
		return nil, nil
	}
	for _, t := range tl.tasks {
		if t.ID == claimedID {
			return t, nil
		}
	}
	return nil, nil
}

// Complete marks a task as completed with the given output.
// After completion, any tasks that depended solely on this one
// become unblocked (their status remains "pending" for claiming).
func (tl *TaskList) Complete(taskID, output string) error {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	for _, t := range tl.tasks {
		if t.ID == taskID {
			t.Status = "completed"
			t.Output = output
			return tl.saveLocked()
		}
	}
	return fmt.Errorf("task not found: %s", taskID)
}

// List returns a snapshot of all tasks.
func (tl *TaskList) List() []*Task {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	out := make([]*Task, len(tl.tasks))
	copy(out, tl.tasks)
	return out
}

// IsBlocked returns true if any of the task's dependencies are incomplete.
func (tl *TaskList) IsBlocked(taskID string) bool {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.isBlockedLocked(taskID)
}

func (tl *TaskList) isBlockedLocked(taskID string) bool {
	var target *Task
	for _, t := range tl.tasks {
		if t.ID == taskID {
			target = t
			break
		}
	}
	if target == nil || len(target.DependsOn) == 0 {
		return false
	}
	complete := make(map[string]bool, len(tl.tasks))
	for _, t := range tl.tasks {
		if t.Status == "completed" {
			complete[t.ID] = true
		}
	}
	for _, dep := range target.DependsOn {
		if !complete[dep] {
			return true
		}
	}
	return false
}

// Save persists the task list to disk atomically.
func (tl *TaskList) Save() error {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.saveLocked()
}

func (tl *TaskList) saveLocked() error {
	data, err := json.MarshalIndent(tl.tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}
	dir := filepath.Dir(tl.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := tl.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, tl.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Load reads the task list from disk.
func (tl *TaskList) Load() error {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	return tl.loadLocked()
}

func (tl *TaskList) loadLocked() error {
	data, err := os.ReadFile(tl.path)
	if err != nil {
		return err
	}
	var tasks []*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("unmarshal tasks: %w", err)
	}
	tl.tasks = tasks
	return nil
}

// flockDo acquires an exclusive flock on the tasks file, runs fn, then releases.
func (tl *TaskList) flockDo(fn func() error) error {
	dir := filepath.Dir(tl.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lockPath := tl.path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock: %w", err)
	}
	defer func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
		f.Close()
	}()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	return fn()
}
