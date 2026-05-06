package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/task"
)

// taskCreateTool creates a new task in the task queue.
type taskCreateTool struct{ q *task.Queue }

func NewTaskCreateTool(q *task.Queue) Tool { return &taskCreateTool{q: q} }

func (t *taskCreateTool) Name() string        { return "TaskCreate" }
func (t *taskCreateTool) IsConcurrencySafe() bool { return true }
func (t *taskCreateTool) IsReadOnly() bool     { return false }
func (t *taskCreateTool) Description() string {
	return "Create a task to track progress on a multi-step operation. " +
		"Tasks appear in the HUD status line. Use for complex work with 3+ steps."
}
func (t *taskCreateTool) PermissionPattern(json.RawMessage) string { return "TaskCreate" }
func (t *taskCreateTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"subject": {"type": "string", "description": "Brief title for the task"},
			"description": {"type": "string", "description": "What needs to be done"},
			"activeForm": {"type": "string", "description": "Present continuous form shown in spinner (e.g. Running tests)"}
		},
		"required": ["subject"]
	}`)
}

func (t *taskCreateTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var p struct {
		Subject     string `json:"subject"`
		Description string `json:"description"`
		ActiveForm  string `json:"activeForm"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, err
	}
	created := t.q.Create(p.Subject, p.Description)
	return &Result{
		Output: fmt.Sprintf("Task %s created: %s", created.ID, created.Subject),
		Title:  "TaskCreate",
	}, nil
}

// taskUpdateTool updates an existing task's status.
type taskUpdateTool struct{ q *task.Queue }

func NewTaskUpdateTool(q *task.Queue) Tool { return &taskUpdateTool{q: q} }

func (t *taskUpdateTool) Name() string        { return "TaskUpdate" }
func (t *taskUpdateTool) IsConcurrencySafe() bool { return true }
func (t *taskUpdateTool) IsReadOnly() bool     { return false }
func (t *taskUpdateTool) Description() string {
	return "Update a task's status. Mark as in_progress when starting, completed when done."
}
func (t *taskUpdateTool) PermissionPattern(json.RawMessage) string { return "TaskUpdate" }
func (t *taskUpdateTool) Parameters() json.RawMessage {
	// Schema enum must match the actual task.Status constants. The
	// previous schema said 'in_progress' but the Go const is 'running',
	// so clients following the schema produced invalid status values
	// that silently cast to Status("in_progress") and broke the state
	// machine. Accept 'in_progress' as an alias for 'running' in the
	// Execute path for backward compatibility with older clients.
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"taskId": {"type": "string", "description": "The task ID to update"},
			"status": {"type": "string", "enum": ["pending", "running", "completed", "failed"]},
			"subject": {"type": "string", "description": "Update the task title"},
			"activeForm": {"type": "string", "description": "Update the spinner text"}
		},
		"required": ["taskId"]
	}`)
}

func (t *taskUpdateTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var p struct {
		TaskID string `json:"taskId"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, err
	}

	if p.TaskID == "" {
		return &Result{
			Output: "Error: taskId is required",
			Title:  "TaskUpdate",
			Error:  fmt.Errorf("taskId is required"),
		}, nil
	}

	// Accept 'in_progress' as an alias for 'running' — older clients
	// and the previous (wrong) schema used that spelling.
	rawStatus := p.Status
	if rawStatus == "in_progress" {
		rawStatus = "running"
	}
	status := task.Status(rawStatus)
	if status == "" {
		status = task.StatusRunning
	}
	// Validate status is one of the known enum values so a typo
	// doesn't silently store an invalid state.
	switch status {
	case task.StatusPending, task.StatusRunning, task.StatusCompleted, task.StatusFailed:
		// ok
	default:
		return &Result{
			Output: fmt.Sprintf("Error: invalid status %q (want pending/running/completed/failed)", p.Status),
			Title:  "TaskUpdate",
			Error:  fmt.Errorf("invalid status %q", p.Status),
		}, nil
	}
	if err := t.q.Update(p.TaskID, status, ""); err != nil {
		return &Result{
			Output: fmt.Sprintf("Error: %v", err),
			Title:  "TaskUpdate",
			Error:  err,
		}, nil
	}
	return &Result{
		Output: fmt.Sprintf("Task %s → %s", p.TaskID, status),
		Title:  "TaskUpdate",
	}, nil
}

// taskListTool lists all tasks.
type taskListTool struct{ q *task.Queue }

func NewTaskListTool(q *task.Queue) Tool { return &taskListTool{q: q} }

func (t *taskListTool) Name() string        { return "TaskList" }
func (t *taskListTool) IsConcurrencySafe() bool { return true }
func (t *taskListTool) IsReadOnly() bool     { return true }
func (t *taskListTool) Description() string  { return "List all tasks and their status." }
func (t *taskListTool) PermissionPattern(json.RawMessage) string { return "TaskList" }
func (t *taskListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *taskListTool) Execute(_ context.Context, _ json.RawMessage) (*Result, error) {
	tasks := t.q.List()
	if len(tasks) == 0 {
		return &Result{Output: "No tasks.", Title: "TaskList"}, nil
	}
	var sb strings.Builder
	for _, tk := range tasks {
		icon := "○"
		switch tk.Status {
		case task.StatusRunning:
			icon = "◐"
		case task.StatusCompleted:
			icon = "●"
		case task.StatusFailed:
			icon = "✗"
		}
		sb.WriteString(fmt.Sprintf("%s %s [%s] %s\n", icon, tk.ID, tk.Status, tk.Subject))
	}
	return &Result{Output: sb.String(), Title: "TaskList"}, nil
}

// taskGetTool gets details of a specific task.
type taskGetTool struct{ q *task.Queue }

func NewTaskGetTool(q *task.Queue) Tool { return &taskGetTool{q: q} }

func (t *taskGetTool) Name() string        { return "TaskGet" }
func (t *taskGetTool) IsConcurrencySafe() bool { return true }
func (t *taskGetTool) IsReadOnly() bool     { return true }
func (t *taskGetTool) Description() string  { return "Get details of a specific task by ID." }
func (t *taskGetTool) PermissionPattern(json.RawMessage) string { return "TaskGet" }
func (t *taskGetTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"taskId": {"type": "string", "description": "The task ID"}
		},
		"required": ["taskId"]
	}`)
}

func (t *taskGetTool) Execute(_ context.Context, input json.RawMessage) (*Result, error) {
	var p struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, err
	}
	tk, ok := t.q.Get(p.TaskID)
	if !ok {
		return &Result{Error: fmt.Errorf("task not found: %s", p.TaskID)}, nil
	}
	out := fmt.Sprintf("ID: %s\nSubject: %s\nStatus: %s\nDescription: %s\nCreated: %s",
		tk.ID, tk.Subject, tk.Status, tk.Description, tk.CreatedAt.Format("15:04:05"))
	return &Result{Output: out, Title: "TaskGet"}, nil
}
