package task

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Status represents the lifecycle state of a task.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// Task represents a background unit of work.
type Task struct {
	ID          string
	Subject     string
	Description string
	Status      Status
	Output      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Queue manages background tasks with thread-safe access.
type Queue struct {
	tasks map[string]*Task
	order []string // preserves insertion order
	mu    sync.Mutex
	seq   int
}

// NewQueue creates an empty task queue.
func NewQueue() *Queue {
	return &Queue{tasks: make(map[string]*Task)}
}

// Create adds a new task in pending status and returns it.
func (q *Queue) Create(subject, description string) *Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.seq++
	now := time.Now()
	t := &Task{
		ID:          fmt.Sprintf("task_%d", q.seq),
		Subject:     subject,
		Description: description,
		Status:      StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	q.tasks[t.ID] = t
	q.order = append(q.order, t.ID)
	return t
}

// Get returns the task with the given ID, if it exists.
func (q *Queue) Get(id string) (*Task, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	t, ok := q.tasks[id]
	return t, ok
}

// Update sets the status and output of an existing task.
func (q *Queue) Update(id string, status Status, output string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	t, ok := q.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Status = status
	t.Output = output
	t.UpdatedAt = time.Now()
	return nil
}

// List returns all tasks in creation order.
func (q *Queue) List() []*Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]*Task, 0, len(q.order))
	for _, id := range q.order {
		if t, ok := q.tasks[id]; ok {
			result = append(result, t)
		}
	}
	return result
}

// Len returns the number of tasks in the queue.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

// Summary returns a one-line status overview.
func (q *Queue) Summary() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.summaryLocked()
}

func (q *Queue) summaryLocked() string {
	counts := map[Status]int{}
	for _, t := range q.tasks {
		counts[t.Status]++
	}
	if len(q.tasks) == 0 {
		return "No tasks."
	}
	var parts []string
	for _, s := range []Status{
		StatusPending, StatusRunning,
		StatusCompleted, StatusFailed,
	} {
		if c := counts[s]; c > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c, s))
		}
	}
	return fmt.Sprintf(
		"Tasks (%d): %s", len(q.tasks), strings.Join(parts, ", "),
	)
}
