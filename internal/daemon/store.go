package daemon

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
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
	MaxCostUSD      float64    `json:"max_cost_usd,omitempty"` // 0 = unlimited
	MaxTurns        int        `json:"max_turns,omitempty"`    // 0 = unlimited
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
		max_cost_usd REAL DEFAULT 0,
		max_turns INTEGER DEFAULT 0,
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
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	// Migrations: additive ALTER TABLE for rows created before the
	// max_cost_usd/max_turns columns landed. SQLite rejects ADD COLUMN
	// if the column already exists, so swallow "duplicate column" errors.
	for _, m := range []string{
		`ALTER TABLE tasks ADD COLUMN max_cost_usd REAL DEFAULT 0`,
		`ALTER TABLE tasks ADD COLUMN max_turns INTEGER DEFAULT 0`,
	} {
		if _, err := s.db.Exec(m); err != nil &&
			!strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
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
		 repo_owner, repo_name, delivery_id, max_cost_usd, max_turns,
		 created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.RepoURL, t.TaskDescription, t.Status, t.AgentConfig,
		t.BranchName, t.Complexity, t.IssueNumber, t.RepoOwner,
		t.RepoName, t.DeliveryID, t.MaxCostUSD, t.MaxTurns,
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
		 error_message, delivery_id, max_cost_usd, max_turns,
		 created_at, updated_at
		 FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// ListTasks returns all tasks ordered by creation time descending.
func (s *Store) ListTasks() ([]*Task, error) {
	rows, err := s.db.Query(
		`SELECT id, repo_url, task_description, status, agent_config,
		 pr_number, pr_url, branch_name, api_cost_usd, complexity,
		 issue_number, repo_owner, repo_name, started_at, completed_at,
		 error_message, delivery_id, max_cost_usd, max_turns,
		 created_at, updated_at
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

// ListTasksByStatus returns tasks with the given status.
//
// Ordering is strictly FIFO (oldest first) by SQLite's internal rowid,
// which is monotonically assigned at INSERT time. This is stronger
// than (created_at, id) because the id is random hex — two tasks
// created in the same second with id "aaa" < "zzz" would tie-break
// lexicographically, not by arrival order. Codex round-E caught that
// subtle queue-position regression; using rowid closes the gap.
func (s *Store) ListTasksByStatus(status string) ([]*Task, error) {
	rows, err := s.db.Query(
		`SELECT id, repo_url, task_description, status, agent_config,
		 pr_number, pr_url, branch_name, api_cost_usd, complexity,
		 issue_number, repo_owner, repo_name, started_at, completed_at,
		 error_message, delivery_id, max_cost_usd, max_turns,
		 created_at, updated_at
		 FROM tasks WHERE status = ?
		 ORDER BY rowid ASC`,
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

// UpdateStatus sets the task status and updates updated_at. The
// WHERE clause refuses to resurrect terminal rows back to an active
// phase — a stale orchestrator writing UpdateStatus("implementing")
// after the task was cancelled must not un-cancel it. Returns nil
// without error when the row is already terminal (idempotent no-op).
func (s *Store) UpdateStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?
		   AND status NOT IN ('merged','failed','closed','cancelled')`,
		status, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Confirm the row exists before returning — a truly missing
		// id is a programmer bug, a terminal row is the benign race.
		var exists int
		err := s.db.QueryRow(
			`SELECT 1 FROM tasks WHERE id = ?`, id,
		).Scan(&exists)
		if err == sql.ErrNoRows {
			return fmt.Errorf("task %q not found", id)
		}
		if err != nil {
			return fmt.Errorf("task %q lookup: %w", id, err)
		}
	}
	return nil
}

// CancelIfActive transitions a task to cancelled if it is in any
// non-terminal state — queued (pending) OR mid-phase (planning,
// implementing, reviewing, testing, awaiting_spec, pr_open). This is
// the handler-path semantics: user stopped a task with no live runner
// (queued, or runner crashed mid-execution leaving the task orphaned).
// We deliberately exclude 'failed' so a genuine failure that raced the
// stop request isn't rewritten as user cancellation — and 'merged',
// 'closed', 'cancelled' for their usual reasons.
//
// Returns (true, nil) if the row was updated; (false, nil) otherwise
// (task unknown, or in a terminal status). Callers wanting to
// distinguish the two cases must pre-read the task.
func (s *Store) CancelIfActive(id string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET status = 'cancelled', error_message = '',
		 completed_at = ?, updated_at = ? WHERE id = ?
		   AND status NOT IN
		     ('merged','failed','closed','cancelled')`,
		now, now, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkCancelled sets status to cancelled, records completion time, and
// clears any error_message left over from a MarkFailed that fired before
// the runner could reclassify the result as a user-cancellation.
//
// The WHERE clause excludes already-terminal statuses so a stop request
// that arrives just after the runner finishes successfully cannot
// silently overwrite a "merged"/"failed"/"closed" task to "cancelled"
// (TOCTOU race between handleStopTask's GetTask and this update).
// Returns nil with no error if the row was already terminal — the
// caller should treat zero-rows-affected as a no-op for that race.
func (s *Store) MarkCancelled(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	// We INTENTIONALLY allow overwriting "failed" → "cancelled":
	// when the orchestrator's SpawnFunc returns ctx.Err on a user
	// cancel, RunTask writes MarkFailed before TaskRunner.Run gets to
	// reclassify. The reclassification is the whole point. We do NOT
	// allow overwriting "merged"/"closed" (real successes) or
	// "cancelled" (already done — keeps this idempotent).
	res, err := s.db.Exec(
		`UPDATE tasks SET status = 'cancelled', error_message = '',
		 completed_at = ?, updated_at = ? WHERE id = ?
		   AND status NOT IN ('merged','closed','cancelled')`,
		now, now, id)
	if err != nil {
		return err
	}
	// Distinguish "task missing" from "task already terminal". The
	// former is a programmer bug; the latter is the benign TOCTOU race.
	if n, _ := res.RowsAffected(); n == 0 {
		var status string
		err := s.db.QueryRow(
			`SELECT status FROM tasks WHERE id = ?`, id,
		).Scan(&status)
		if err == sql.ErrNoRows {
			return fmt.Errorf("task %q not found", id)
		}
		if err != nil {
			// Surface real DB failures rather than silently pretending
			// the cancel succeeded.
			return fmt.Errorf("task %q diagnostic query: %w", id, err)
		}
		// err == nil and row exists: already terminal — no-op.
	}
	return nil
}

// MarkFailed sets status to failed with an error message. Refuses to
// overwrite 'merged', 'closed', or 'cancelled' — a slow orchestrator
// goroutine writing MarkFailed after the user has cancelled the task
// (MarkCancelled ran, or the runner reclassified) must not undo that.
// Noops silently on terminal rows.
func (s *Store) MarkFailed(id, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET status = 'failed', error_message = ?,
		 completed_at = ?, updated_at = ? WHERE id = ?
		   AND status NOT IN ('merged','closed','cancelled')`,
		errMsg, now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		err := s.db.QueryRow(
			`SELECT 1 FROM tasks WHERE id = ?`, id,
		).Scan(&exists)
		if err == sql.ErrNoRows {
			return fmt.Errorf("task %q not found", id)
		}
		if err != nil {
			return fmt.Errorf("task %q lookup: %w", id, err)
		}
	}
	return nil
}

// MarkCompleted sets status to merged. Refuses to overwrite
// 'failed', 'closed', or 'cancelled' — a lagging success write must
// not clobber an earlier terminal verdict. Idempotent on 'merged'.
func (s *Store) MarkCompleted(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET status = 'merged', completed_at = ?,
		 updated_at = ? WHERE id = ?
		   AND status NOT IN ('failed','closed','cancelled','merged')`,
		now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		err := s.db.QueryRow(
			`SELECT 1 FROM tasks WHERE id = ?`, id,
		).Scan(&exists)
		if err == sql.ErrNoRows {
			return fmt.Errorf("task %q not found", id)
		}
		if err != nil {
			return fmt.Errorf("task %q lookup: %w", id, err)
		}
	}
	return nil
}

// MarkStarted records the started_at timestamp for a task. Refuses
// to set started_at on a terminal row — a stale orchestrator path
// (e.g. one that raced Stop) must not stamp a start time on a task
// that was already cancelled/failed/merged.
func (s *Store) MarkStarted(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE tasks SET started_at = ?, updated_at = ? WHERE id = ?
		   AND status NOT IN ('merged','failed','closed','cancelled')`,
		now, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		err := s.db.QueryRow(
			`SELECT 1 FROM tasks WHERE id = ?`, id,
		).Scan(&exists)
		if err == sql.ErrNoRows {
			return fmt.Errorf("task %q not found", id)
		}
		if err != nil {
			return fmt.Errorf("task %q lookup: %w", id, err)
		}
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
		&t.DeliveryID, &t.MaxCostUSD, &t.MaxTurns,
		&createdAt, &updatedAt,
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

// CountPendingBefore returns the number of pending tasks that are
// ahead of the given task in dispatch order. Used for queue position.
//
// Uses SQLite's rowid as the ordering key — it's monotonic at INSERT
// time and matches ListTasksByStatus's FIFO ordering. created_at at
// RFC3339 second precision is too coarse for bursty creates; the id
// is random hex (not a timestamp) so it can't tie-break correctly.
func (s *Store) CountPendingBefore(taskID string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM tasks
		 WHERE status = 'pending'
		   AND rowid < (SELECT rowid FROM tasks WHERE id = ?)`,
		taskID).Scan(&count)
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
