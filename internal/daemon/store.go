package daemon

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Task represents a daemon task record.
type Task struct {
	ID              string     `json:"id"`
	RepoURL         string     `json:"repo_url"`
	TaskDescription string     `json:"task_description"`
	Status          string     `json:"status"` // pending|planning|implementing|reviewing|testing|pr_open|merged|closed|failed|cancelled
	AgentConfig     string     `json:"agent_config,omitempty"`
	PRNumber        int        `json:"pr_number,omitempty"`
	PRURL           string     `json:"pr_url,omitempty"`
	BranchName      string     `json:"branch_name,omitempty"`
	APICostUSD      float64    `json:"api_cost_usd"`
	Complexity      string     `json:"complexity,omitempty"`
	IssueNumber     int        `json:"issue_number,omitempty"`
	RepoOwner       string     `json:"repo_owner,omitempty"`
	RepoName        string     `json:"repo_name,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	DeliveryID      string     `json:"delivery_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// TaskEvent represents a progress event for a task.
type TaskEvent struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	EventType string    `json:"event_type"`
	Data      string    `json:"data,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store provides SQLite persistence for daemon tasks.
type Store struct {
	db *sql.DB
}

// NewStore opens (or creates) the SQLite database and initializes schema.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("daemon store open: %w", err)
	}
	// WAL mode for concurrent reads during task execution.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("daemon store pragma %q: %w", p, err)
		}
	}
	// SQLite only supports one writer at a time. Limiting open
	// connections prevents "database is locked" under concurrency.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.createSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		repo_url TEXT NOT NULL,
		task_description TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		agent_config TEXT DEFAULT '',
		pr_number INTEGER DEFAULT 0,
		pr_url TEXT DEFAULT '',
		branch_name TEXT DEFAULT '',
		api_cost_usd REAL DEFAULT 0,
		complexity TEXT DEFAULT '',
		issue_number INTEGER DEFAULT 0,
		repo_owner TEXT DEFAULT '',
		repo_name TEXT DEFAULT '',
		started_at TEXT,
		completed_at TEXT,
		error_message TEXT DEFAULT '',
		delivery_id TEXT DEFAULT '',
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	);
	CREATE TABLE IF NOT EXISTS task_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		event_type TEXT NOT NULL,
		data TEXT DEFAULT '',
		created_at TEXT DEFAULT (datetime('now'))
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_id
		ON tasks(delivery_id) WHERE delivery_id != '';
	CREATE INDEX IF NOT EXISTS idx_task_events_task
		ON task_events(task_id, id);
	CREATE TABLE IF NOT EXISTS checkpoints (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		phase TEXT NOT NULL,
		phase_number INTEGER NOT NULL DEFAULT 0,
		git_sha TEXT DEFAULT '',
		test_summary TEXT DEFAULT '',
		cost_so_far REAL DEFAULT 0,
		files_changed INTEGER DEFAULT 0,
		created_at TEXT DEFAULT (datetime('now'))
	);
	CREATE INDEX IF NOT EXISTS idx_checkpoints_task
		ON checkpoints(task_id, phase_number);
	`
	_, err := s.db.Exec(schema)
	return err
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read: %v", err))
	}
	return hex.EncodeToString(b)
}

// CreateTask inserts a new task with a generated ID.
func (s *Store) CreateTask(t *Task) error {
	t.ID = newID()
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO tasks (id, repo_url, task_description, status,
		 agent_config, branch_name, complexity, issue_number,
		 repo_owner, repo_name, delivery_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.RepoURL, t.TaskDescription, t.Status, t.AgentConfig,
		t.BranchName, t.Complexity, t.IssueNumber, t.RepoOwner,
		t.RepoName, t.DeliveryID,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	return err
}

// GetTask retrieves a task by ID.
func (s *Store) GetTask(id string) (*Task, error) {
	row := s.db.QueryRow(
		`SELECT id, repo_url, task_description, status, agent_config,
		 pr_number, pr_url, branch_name, api_cost_usd, complexity,
		 issue_number, repo_owner, repo_name, started_at, completed_at,
		 error_message, delivery_id, created_at, updated_at
		 FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// ListTasks returns all tasks ordered by creation time descending.
func (s *Store) ListTasks() ([]*Task, error) {
	rows, err := s.db.Query(
		`SELECT id, repo_url, task_description, status, agent_config,
		 pr_number, pr_url, branch_name, api_cost_usd, complexity,
		 issue_number, repo_owner, repo_name, started_at, completed_at,
		 error_message, delivery_id, created_at, updated_at
		 FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListTasksByStatus returns tasks with the given status,
// ordered by creation time descending.
func (s *Store) ListTasksByStatus(status string) ([]*Task, error) {
	rows, err := s.db.Query(
		`SELECT id, repo_url, task_description, status, agent_config,
		 pr_number, pr_url, branch_name, api_cost_usd, complexity,
		 issue_number, repo_owner, repo_name, started_at, completed_at,
		 error_message, delivery_id, created_at, updated_at
		 FROM tasks WHERE status = ? ORDER BY created_at DESC`,
		status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// UpdateStatus sets the task status and updates updated_at.
func (s *Store) UpdateStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

// MarkFailed sets status to failed with an error message.
func (s *Store) MarkFailed(id, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET status = 'failed', error_message = ?,
		 completed_at = ?, updated_at = ? WHERE id = ?`,
		errMsg, now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

// MarkCompleted sets status to merged and records completion time.
func (s *Store) MarkCompleted(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET status = 'merged', completed_at = ?,
		 updated_at = ? WHERE id = ?`,
		now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

// MarkStarted records the started_at timestamp for a task.
func (s *Store) MarkStarted(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET started_at = ?, updated_at = ? WHERE id = ?`,
		now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}

// AppendEvent records a progress event for a task.
func (s *Store) AppendEvent(taskID, eventType, data string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(
		`INSERT INTO task_events (task_id, event_type, data, created_at)
		 VALUES (?, ?, ?, ?)`,
		taskID, eventType, data, now)
	return err
}

// ListEvents returns events for a task, optionally after a given ID.
func (s *Store) ListEvents(taskID string, afterID int64) ([]*TaskEvent, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, event_type, data, created_at
		 FROM task_events WHERE task_id = ? AND id > ?
		 ORDER BY id ASC`, taskID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*TaskEvent
	for rows.Next() {
		var e TaskEvent
		var createdAt sql.NullString
		if err := rows.Scan(
			&e.ID, &e.TaskID, &e.EventType, &e.Data, &createdAt,
		); err != nil {
			return nil, err
		}
		e.CreatedAt = parseTime(createdAt)
		events = append(events, &e)
	}
	return events, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

// parseTime handles both RFC3339 and SQLite datetime format.
func parseTime(s sql.NullString) time.Time {
	if !s.Valid || s.String == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s.String); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s.String); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func scanTask(s scanner) (*Task, error) {
	var t Task
	var startedAt, completedAt, createdAt, updatedAt sql.NullString
	err := s.Scan(
		&t.ID, &t.RepoURL, &t.TaskDescription, &t.Status,
		&t.AgentConfig, &t.PRNumber, &t.PRURL, &t.BranchName,
		&t.APICostUSD, &t.Complexity, &t.IssueNumber, &t.RepoOwner,
		&t.RepoName, &startedAt, &completedAt, &t.ErrorMessage,
		&t.DeliveryID, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if parsed := parseTime(startedAt); !parsed.IsZero() {
		t.StartedAt = &parsed
	}
	if parsed := parseTime(completedAt); !parsed.IsZero() {
		t.CompletedAt = &parsed
	}
	t.CreatedAt = parseTime(createdAt)
	t.UpdatedAt = parseTime(updatedAt)
	return &t, nil
}

// Checkpoint represents a named phase snapshot.
type Checkpoint struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	Phase        string    `json:"phase"`
	PhaseNumber  int       `json:"phase_number"`
	GitSHA       string    `json:"git_sha"`
	TestSummary  string    `json:"test_summary"`
	CostSoFar    float64   `json:"cost_so_far"`
	FilesChanged int       `json:"files_changed"`
	CreatedAt    time.Time `json:"created_at"`
}

// CreateCheckpoint inserts a checkpoint with a generated ID.
func (s *Store) CreateCheckpoint(cp *Checkpoint) error {
	cp.ID = newID()
	now := time.Now().UTC()
	cp.CreatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO checkpoints
		 (id, task_id, phase, phase_number, git_sha,
		  test_summary, cost_so_far, files_changed, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cp.ID, cp.TaskID, cp.Phase, cp.PhaseNumber,
		cp.GitSHA, cp.TestSummary, cp.CostSoFar,
		cp.FilesChanged, now.Format(time.RFC3339),
	)
	return err
}

// ListCheckpoints returns checkpoints for a task ordered by
// phase_number ascending.
func (s *Store) ListCheckpoints(taskID string) ([]*Checkpoint, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, phase, phase_number, git_sha,
		 test_summary, cost_so_far, files_changed, created_at
		 FROM checkpoints WHERE task_id = ?
		 ORDER BY phase_number ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cps []*Checkpoint
	for rows.Next() {
		cp, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		cps = append(cps, cp)
	}
	return cps, rows.Err()
}

// GetCheckpoint retrieves a single checkpoint by ID.
func (s *Store) GetCheckpoint(id string) (*Checkpoint, error) {
	row := s.db.QueryRow(
		`SELECT id, task_id, phase, phase_number, git_sha,
		 test_summary, cost_so_far, files_changed, created_at
		 FROM checkpoints WHERE id = ?`, id)
	return scanCheckpoint(row)
}

// CountPendingBefore returns the number of pending tasks created
// before the given task. Used for queue position calculation.
func (s *Store) CountPendingBefore(taskID string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM tasks
		 WHERE status = 'pending'
		 AND created_at < (SELECT created_at FROM tasks WHERE id = ?)
		 AND id != ?`, taskID, taskID).Scan(&count)
	return count, err
}

func scanCheckpoint(s scanner) (*Checkpoint, error) {
	var cp Checkpoint
	var createdAt sql.NullString
	err := s.Scan(
		&cp.ID, &cp.TaskID, &cp.Phase, &cp.PhaseNumber,
		&cp.GitSHA, &cp.TestSummary, &cp.CostSoFar,
		&cp.FilesChanged, &createdAt,
	)
	if err != nil {
		return nil, err
	}
	cp.CreatedAt = parseTime(createdAt)
	return &cp, nil
}
