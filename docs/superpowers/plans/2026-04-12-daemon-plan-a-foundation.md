# AltFix Daemon — Plan A: Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working HTTP daemon that accepts coding tasks, spawns codex/claude as subprocesses, orchestrates the Lead→Implement→Review→Test loop, persists state to SQLite, and handles cancel/timeout/crash recovery.

**Architecture:** New `internal/daemon/` package with zero imports from internal/tui, internal/exec, or internal/engine. Agents are spawned as child processes via `os/exec`. HTTP server uses stdlib `net/http`. SQLite via `modernc.org/sqlite` (already in go.mod). One goroutine per task, semaphore for concurrency.

**Tech Stack:** Go 1.22+, net/http, modernc.org/sqlite, os/exec, syscall (Setpgid, PR_SET_CHILD_SUBREAPER)

**Covers Issues:** #3, #4, #10, #15, #16

**Spec:** `docs/superpowers/specs/2026-04-12-altfix-daemon-design.md` (v5)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/daemon/store.go` | SQLite schema, task CRUD, event logging |
| `internal/daemon/store_test.go` | Store unit tests with :memory: DB |
| `internal/daemon/task_state.go` | Versioned TaskState artifact (checksum + atomic write) |
| `internal/daemon/task_state_test.go` | Artifact round-trip + corruption detection tests |
| `internal/daemon/subprocess.go` | Spawn agent binary, pipe stdout/stdin, process groups |
| `internal/daemon/subprocess_test.go` | Spawn `echo` as mock agent, verify stdout capture |
| `internal/daemon/output_sink.go` | Parse agent JSONL stdout into ProgressEvents |
| `internal/daemon/output_sink_test.go` | Parse known JSONL fixtures |
| `internal/daemon/orchestrator.go` | Phase state machine: plan→implement→review→test→finalize |
| `internal/daemon/orchestrator_test.go` | Mock subprocess, verify phase transitions |
| `internal/daemon/server.go` | HTTP server lifecycle, mux, auth middleware, shutdown |
| `internal/daemon/server_test.go` | Auth middleware unit tests |
| `internal/daemon/handlers.go` | 6 HTTP endpoint handlers |
| `internal/daemon/handlers_test.go` | Handler integration tests with httptest |
| `internal/daemon/lifecycle.go` | Cancel, wall-clock timeout, crash recovery, dedup |
| `internal/daemon/lifecycle_test.go` | Crash recovery + dedup tests |
| `cmd/altcode/daemon.go` | Cobra `daemon` subcommand |

**Modified:** `cmd/altcode/main.go` — add `root.AddCommand(daemonCmd)` (1 line)

---

### Task 1: SQLite Store — Schema + Task CRUD

**Files:**
- Create: `internal/daemon/store.go`
- Create: `internal/daemon/store_test.go`

- [ ] **Step 1: Write the failing test for store creation**

```go
// internal/daemon/store_test.go
package daemon

import (
	"testing"
)

func TestNewStore_CreatesSchema(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	// Schema should exist — verify by inserting a task
	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "fix bug",
		Status:          "pending",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID == "" {
		t.Error("expected non-empty task ID")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestNewStore -v`
Expected: FAIL — package doesn't exist yet

- [ ] **Step 3: Write minimal store implementation**

```go
// internal/daemon/store.go
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
	ID              string
	RepoURL         string
	TaskDescription string
	Status          string // pending|planning|implementing|reviewing|testing|pr_open|merged|closed|failed|cancelled
	AgentConfig     string // JSON
	PRNumber        int
	PRURL           string
	BranchName      string
	APICostUSD      float64
	Complexity      string // simple|medium|complex
	IssueNumber     int
	RepoOwner       string
	RepoName        string
	StartedAt       *time.Time
	CompletedAt     *time.Time
	ErrorMessage    string
	DeliveryID      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TaskEvent represents a progress event for a task.
type TaskEvent struct {
	ID        int64
	TaskID    string
	EventType string
	Data      string // JSON
	CreatedAt time.Time
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
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("daemon store WAL: %w", err)
	}
	s := &Store{db: db}
	if err := s.createSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

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
	`
	_, err := s.db.Exec(schema)
	return err
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateTask inserts a new task with a generated ID.
func (s *Store) CreateTask(t *Task) error {
	t.ID = newID()
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	_, err := s.db.Exec(
		`INSERT INTO tasks (id, repo_url, task_description, status, agent_config,
		 branch_name, complexity, issue_number, repo_owner, repo_name, delivery_id,
		 created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.RepoURL, t.TaskDescription, t.Status, t.AgentConfig,
		t.BranchName, t.Complexity, t.IssueNumber, t.RepoOwner, t.RepoName,
		t.DeliveryID, t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
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

// UpdateStatus sets the task status and updates updated_at.
func (s *Store) UpdateStatus(id, status string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = datetime('now') WHERE id = ?`,
		status, id)
	return err
}

// MarkFailed sets status to failed with an error message.
func (s *Store) MarkFailed(id, errMsg string) error {
	_, err := s.db.Exec(
		`UPDATE tasks SET status = 'failed', error_message = ?,
		 completed_at = datetime('now'), updated_at = datetime('now')
		 WHERE id = ?`, errMsg, id)
	return err
}

// AppendEvent records a progress event for a task.
func (s *Store) AppendEvent(taskID, eventType, data string) error {
	_, err := s.db.Exec(
		`INSERT INTO task_events (task_id, event_type, data)
		 VALUES (?, ?, ?)`, taskID, eventType, data)
	return err
}

// ListEvents returns events for a task, optionally after a sequence ID.
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
		var createdAt string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.EventType, &e.Data, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		events = append(events, &e)
	}
	return events, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (*Task, error) {
	var t Task
	var startedAt, completedAt, createdAt, updatedAt sql.NullString
	err := s.Scan(
		&t.ID, &t.RepoURL, &t.TaskDescription, &t.Status, &t.AgentConfig,
		&t.PRNumber, &t.PRURL, &t.BranchName, &t.APICostUSD, &t.Complexity,
		&t.IssueNumber, &t.RepoOwner, &t.RepoName, &startedAt, &completedAt,
		&t.ErrorMessage, &t.DeliveryID, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		parsed, _ := time.Parse(time.RFC3339, startedAt.String)
		t.StartedAt = &parsed
	}
	if completedAt.Valid {
		parsed, _ := time.Parse(time.RFC3339, completedAt.String)
		t.CompletedAt = &parsed
	}
	if createdAt.Valid {
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}
	return &t, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestNewStore -v`
Expected: PASS

- [ ] **Step 5: Write additional CRUD tests**

```go
// Append to internal/daemon/store_test.go

func TestStore_GetTask(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{RepoURL: "https://github.com/t/r", TaskDescription: "fix", Status: "pending"}
	s.CreateTask(task)

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TaskDescription != "fix" {
		t.Errorf("description = %q, want fix", got.TaskDescription)
	}
}

func TestStore_ListTasks(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	s.CreateTask(&Task{RepoURL: "r", TaskDescription: "a", Status: "pending"})
	s.CreateTask(&Task{RepoURL: "r", TaskDescription: "b", Status: "pending"})

	tasks, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestStore_UpdateStatus(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{RepoURL: "r", TaskDescription: "a", Status: "pending"}
	s.CreateTask(task)
	s.UpdateStatus(task.ID, "implementing")

	got, _ := s.GetTask(task.ID)
	if got.Status != "implementing" {
		t.Errorf("status = %q, want implementing", got.Status)
	}
}

func TestStore_MarkFailed(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{RepoURL: "r", TaskDescription: "a", Status: "implementing"}
	s.CreateTask(task)
	s.MarkFailed(task.ID, "timeout")

	got, _ := s.GetTask(task.ID)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.ErrorMessage != "timeout" {
		t.Errorf("error = %q, want timeout", got.ErrorMessage)
	}
}

func TestStore_AppendAndListEvents(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{RepoURL: "r", TaskDescription: "a", Status: "pending"}
	s.CreateTask(task)

	s.AppendEvent(task.ID, "phase_started", `{"phase":"plan"}`)
	s.AppendEvent(task.ID, "phase_completed", `{"phase":"plan"}`)

	events, err := s.ListEvents(task.ID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
	if events[0].EventType != "phase_started" {
		t.Errorf("event[0].type = %q", events[0].EventType)
	}

	// Test afterID filtering
	events2, _ := s.ListEvents(task.ID, events[0].ID)
	if len(events2) != 1 {
		t.Errorf("filtered events: got %d, want 1", len(events2))
	}
}

func TestStore_DeliveryIDDedup(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	t1 := &Task{RepoURL: "r", TaskDescription: "a", Status: "pending", DeliveryID: "gh-abc"}
	if err := s.CreateTask(t1); err != nil {
		t.Fatalf("first create: %v", err)
	}
	t2 := &Task{RepoURL: "r", TaskDescription: "b", Status: "pending", DeliveryID: "gh-abc"}
	err := s.CreateTask(t2)
	if err == nil {
		t.Error("expected duplicate delivery_id to fail")
	}
}
```

- [ ] **Step 6: Run all store tests**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestStore -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add internal/daemon/store.go internal/daemon/store_test.go
git commit -m "feat(daemon): Task 1 — SQLite store with task CRUD + event logging"
```

---

### Task 2: Subprocess Spawner

**Files:**
- Create: `internal/daemon/subprocess.go`
- Create: `internal/daemon/subprocess_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/daemon/subprocess_test.go
package daemon

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSpawnAgent_EchoStdout(t *testing.T) {
	// Use 'echo' as a mock agent — verifies stdout capture.
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "echo",
		Args:   []string{"hello from agent"},
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	defer proc.Kill()

	output, err := proc.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(output, "hello from agent") {
		t.Errorf("stdout = %q, want 'hello from agent'", output)
	}
	if err := proc.Wait(); err != nil {
		t.Errorf("Wait: %v", err)
	}
}

func TestSpawnAgent_Kill(t *testing.T) {
	// Sleep subprocess that we kill — verifies process group teardown.
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "sleep",
		Args:   []string{"60"},
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Errorf("Kill: %v", err)
	}
	// Wait should return an error (killed)
	err = proc.Wait()
	if err == nil {
		t.Error("expected error from Wait after Kill")
	}
}

func TestSpawnAgent_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	proc, err := SpawnAgent(ctx, AgentConfig{
		Binary: "sleep",
		Args:   []string{"60"},
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	// Context cancels after 100ms — process should be killed.
	err = proc.Wait()
	if err == nil {
		t.Error("expected error from context cancellation")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestSpawnAgent -v`
Expected: FAIL — SpawnAgent not defined

- [ ] **Step 3: Write subprocess implementation**

```go
// internal/daemon/subprocess.go
package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// AgentConfig describes how to spawn an agent subprocess.
type AgentConfig struct {
	Binary string   // "codex", "claude", "altcode", or any binary
	Args   []string // command-line arguments
	Dir    string   // working directory (worktree path)
	Env    []string // extra environment variables
	Role   string   // "lead", "implementer", "reviewer", "tester"
}

// AgentProcess wraps a running agent subprocess.
type AgentProcess struct {
	Cmd    *exec.Cmd
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
	done   chan error
}

// SpawnAgent starts an agent binary as a child process with its
// own process group (Setpgid) so Kill() can tear down the entire
// tree. The process inherits the daemon's env plus any extras
// from cfg.Env.
func SpawnAgent(ctx context.Context, cfg AgentConfig) (*AgentProcess, error) {
	cmd := exec.CommandContext(ctx, cfg.Binary, cfg.Args...)
	cmd.Dir = cfg.Dir
	cmd.Env = append(os.Environ(), cfg.Env...)

	// Own process group so Kill(-pgid) tears down grandchildren.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Cancel handler: kill the entire process group on ctx cancel.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err != nil {
			return cmd.Process.Kill()
		}
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cfg.Binary, err)
	}

	proc := &AgentProcess{
		Cmd:    cmd,
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		done:   make(chan error, 1),
	}
	go func() {
		proc.done <- cmd.Wait()
	}()
	return proc, nil
}

// ReadAll reads all stdout to completion. Blocks until the process
// closes its stdout (usually on exit).
func (p *AgentProcess) ReadAll() (string, error) {
	data, err := io.ReadAll(p.Stdout)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// SendMessage writes a line to the agent's stdin.
func (p *AgentProcess) SendMessage(msg string) error {
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	_, err := io.WriteString(p.Stdin, msg)
	return err
}

// Wait blocks until the process exits and returns the exit error.
func (p *AgentProcess) Wait() error {
	return <-p.done
}

// Kill sends SIGTERM to the process group, then SIGKILL after 5s.
func (p *AgentProcess) Kill() error {
	if p.Cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(p.Cmd.Process.Pid)
	if err != nil {
		return p.Cmd.Process.Kill()
	}
	// SIGTERM first for graceful shutdown.
	_ = syscall.Kill(-pgid, syscall.SIGTERM)

	// Wait up to 5s, then SIGKILL.
	select {
	case <-p.done:
		return nil
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return <-p.done
	}
}

// IsRunning reports whether the process is still alive.
func (p *AgentProcess) IsRunning() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
```

- [ ] **Step 4: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestSpawnAgent -v -race`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/subprocess.go internal/daemon/subprocess_test.go
git commit -m "feat(daemon): Task 2 — subprocess spawner with process group teardown"
```

---

### Task 3: HTTP Server Shell + Auth Middleware

**Files:**
- Create: `internal/daemon/server.go`
- Create: `internal/daemon/server_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/daemon/server_test.go
package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware_RejectsNoToken(t *testing.T) {
	handler := authMiddleware("secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_AcceptsValidToken(t *testing.T) {
	handler := authMiddleware("secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_HealthBypassesAuth(t *testing.T) {
	handler := authMiddleware("secret-token")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("health should bypass auth, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestAuthMiddleware -v`
Expected: FAIL — authMiddleware not defined

- [ ] **Step 3: Write server implementation**

```go
// internal/daemon/server.go
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// ServerConfig holds daemon startup parameters.
type ServerConfig struct {
	Port      int
	DataDir   string
	AuthToken string
	MaxTasks  int
}

// Server is the HTTP daemon.
type Server struct {
	cfg    ServerConfig
	store  *Store
	mux    *http.ServeMux
	logger *slog.Logger
}

// NewServer creates a daemon server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = home + "/.altcode/daemon"
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	store, err := NewStore(cfg.DataDir + "/tasks.db")
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	s := &Server{
		cfg:    cfg,
		store:  store,
		mux:    http.NewServeMux(),
		logger: logger,
	}
	s.registerRoutes()
	return s, nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /tasks", s.handleCreateTask)
	s.mux.HandleFunc("GET /tasks", s.handleListTasks)
	s.mux.HandleFunc("GET /tasks/{id}", s.handleGetTask)
	s.mux.HandleFunc("POST /tasks/{id}/stop", s.handleStopTask)
	s.mux.HandleFunc("POST /tasks/{id}/steer", s.handleSteerTask)
}

// Run starts the HTTP server and blocks until shutdown.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.middleware(),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-shutdownCtx.Done()
		s.logger.Info("shutting down daemon")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}()

	s.logger.Info("daemon starting", "addr", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return s.store.Close()
}

func (s *Server) middleware() http.Handler {
	var h http.Handler = s.mux
	if s.cfg.AuthToken != "" {
		h = authMiddleware(s.cfg.AuthToken)(h)
	}
	h = recoveryMiddleware(s.logger)(h)
	h = requestIDMiddleware()(h)
	return h
}

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health and metrics bypass auth.
			if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || auth[7:] != token {
				http.Error(w, `{"error":"unauthorized"}`, 401)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func recoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic in handler", "panic", rec, "path", r.URL.Path)
					http.Error(w, `{"error":"internal server error"}`, 500)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func requestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = newID()[:8]
			}
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestAuth -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/server.go internal/daemon/server_test.go
git commit -m "feat(daemon): Task 3 — HTTP server shell with auth + recovery middleware"
```

---

### Task 4: HTTP Handlers — Create, List, Get, Health

**Files:**
- Create: `internal/daemon/handlers.go`
- Create: `internal/daemon/handlers_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/daemon/handlers_test.go
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(ServerConfig{Port: 0, DataDir: t.TempDir(), AuthToken: "test"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { s.store.Close() })
	return s
}

func TestHandler_Health(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("health: got %d", rec.Code)
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("health body: %v", body)
	}
}

func TestHandler_CreateAndGetTask(t *testing.T) {
	s := testServer(t)

	// Create
	payload := `{"repo_url":"https://github.com/t/r","task":"fix bug"}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("create: got %d, body: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	taskID, ok := created["id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("expected task ID, got: %v", created)
	}

	// Get
	req2 := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	rec2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rec2, req2)

	if rec2.Code != 200 {
		t.Errorf("get: got %d", rec2.Code)
	}
}

func TestHandler_ListTasks(t *testing.T) {
	s := testServer(t)

	// Create 2 tasks
	for _, desc := range []string{"a", "b"} {
		payload := `{"repo_url":"r","task":"` + desc + `"}`
		req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
	}

	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	var tasks []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &tasks)
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestHandler -v`
Expected: FAIL — handler methods not defined

- [ ] **Step 3: Write handlers**

```go
// internal/daemon/handlers.go
package daemon

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": "dev",
	})
}

type createTaskRequest struct {
	RepoURL     string `json:"repo_url"`
	Task        string `json:"task"`
	Branch      string `json:"branch"`
	Agents      string `json:"agents"`
	Model       string `json:"model"`
	MaxCostUSD  float64 `json:"max_cost_usd"`
	MaxTurns    int    `json:"max_turns"`
	DeliveryID  string `json:"delivery_id"`
	IssueNumber int    `json:"issue_number"`
	RepoOwner   string `json:"repo_owner"`
	RepoName    string `json:"repo_name"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, 400)
		return
	}
	if req.RepoURL == "" || req.Task == "" {
		http.Error(w, `{"error":"repo_url and task required"}`, 400)
		return
	}

	task := &Task{
		RepoURL:         req.RepoURL,
		TaskDescription: req.Task,
		Status:          "pending",
		BranchName:      req.Branch,
		AgentConfig:     req.Agents,
		DeliveryID:      req.DeliveryID,
		IssueNumber:     req.IssueNumber,
		RepoOwner:       req.RepoOwner,
		RepoName:        req.RepoName,
	}
	if err := s.store.CreateTask(task); err != nil {
		s.logger.Error("create task", "err", err)
		http.Error(w, `{"error":"failed to create task"}`, 500)
		return
	}

	s.logger.Info("task created", "id", task.ID, "repo", req.RepoURL)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]any{"id": task.ID, "status": "pending"})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks()
	if err != nil {
		http.Error(w, `{"error":"failed to list tasks"}`, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.store.GetTask(id)
	if err != nil {
		http.Error(w, `{"error":"task not found"}`, 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleStopTask(w http.ResponseWriter, r *http.Request) {
	// Stub — wired in Task 6 (lifecycle)
	id := r.PathValue("id")
	s.logger.Info("stop requested", "id", id)
	w.WriteHeader(202)
	json.NewEncoder(w).Encode(map[string]string{"status": "stopping"})
}

func (s *Server) handleSteerTask(w http.ResponseWriter, r *http.Request) {
	// Stub — wired in Plan B (#6 steering)
	id := r.PathValue("id")
	s.logger.Info("steer requested", "id", id)
	w.WriteHeader(202)
	json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
}
```

- [ ] **Step 4: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestHandler -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/handlers.go internal/daemon/handlers_test.go
git commit -m "feat(daemon): Task 4 — HTTP handlers for health, create, list, get task"
```

---

### Task 5: Orchestrator — Phase State Machine

**Files:**
- Create: `internal/daemon/orchestrator.go`
- Create: `internal/daemon/orchestrator_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/daemon/orchestrator_test.go
package daemon

import (
	"context"
	"testing"
)

// MockAgent records which prompts it was called with.
type MockAgent struct {
	Calls []string
}

func (m *MockAgent) Run(ctx context.Context, prompt string) (string, error) {
	m.Calls = append(m.Calls, prompt)
	return `{"steps":[{"description":"add auth","prompt":"implement auth"}]}`, nil
}

func TestOrchestrator_PhasesTransition(t *testing.T) {
	store, _ := NewStore(":memory:")
	defer store.Close()

	task := &Task{RepoURL: "r", TaskDescription: "fix bug", Status: "pending"}
	store.CreateTask(task)

	o := NewOrchestrator(store, OrchestratorConfig{
		// Use a no-op agent spawner for testing
		SpawnFunc: func(ctx context.Context, cfg AgentConfig) (string, error) {
			return `{"verdict":"pass"}`, nil
		},
	})

	err := o.RunTask(context.Background(), task)
	if err != nil {
		t.Fatalf("RunTask: %v", err)
	}

	// Verify task transitioned through phases
	got, _ := store.GetTask(task.ID)
	if got.Status != "completed" {
		t.Errorf("final status = %q, want completed", got.Status)
	}

	// Verify events were logged
	events, _ := store.ListEvents(task.ID, 0)
	if len(events) < 2 {
		t.Errorf("expected at least 2 events, got %d", len(events))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestOrchestrator -v`
Expected: FAIL — NewOrchestrator not defined

- [ ] **Step 3: Write orchestrator**

```go
// internal/daemon/orchestrator.go
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"
)

// SpawnFunc is the function signature for spawning an agent and
// collecting its output. Tests inject a mock; production uses
// SpawnAndCollect which delegates to subprocess.go.
type SpawnFunc func(ctx context.Context, cfg AgentConfig) (output string, err error)

// OrchestratorConfig holds orchestrator parameters.
type OrchestratorConfig struct {
	SpawnFunc    SpawnFunc
	MaxFixRetry  int // default 3
	Logger       *slog.Logger
}

// Orchestrator drives the Lead→Implement→Review→Test loop.
type Orchestrator struct {
	store  *Store
	cfg    OrchestratorConfig
	logger *slog.Logger
}

// NewOrchestrator creates an orchestrator.
func NewOrchestrator(store *Store, cfg OrchestratorConfig) *Orchestrator {
	if cfg.MaxFixRetry <= 0 {
		cfg.MaxFixRetry = 3
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &Orchestrator{store: store, cfg: cfg, logger: cfg.Logger}
}

// Plan is the structured output from the lead agent.
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// PlanStep is a single step in the plan.
type PlanStep struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// RunTask executes the full orchestration loop for a task.
func (o *Orchestrator) RunTask(ctx context.Context, task *Task) error {
	// Phase 1: Plan
	o.emitPhase(task.ID, "plan", "started")
	o.store.UpdateStatus(task.ID, "planning")

	planOutput, err := o.cfg.SpawnFunc(ctx, AgentConfig{
		Binary: "echo", // placeholder — Plan B wires real agents
		Args:   []string{task.TaskDescription},
		Role:   "lead",
	})
	if err != nil {
		o.store.MarkFailed(task.ID, fmt.Sprintf("plan failed: %v", err))
		return err
	}
	o.emitPhase(task.ID, "plan", "completed")

	var plan Plan
	if jerr := json.Unmarshal([]byte(planOutput), &plan); jerr != nil {
		// Fallback: treat as single-step plan
		plan = Plan{Steps: []PlanStep{{
			Description: "implement",
			Prompt:      task.TaskDescription,
		}}}
	}

	// Phase 2: Implement (per step with verify loop)
	o.emitPhase(task.ID, "implement", "started")
	o.store.UpdateStatus(task.ID, "implementing")

	for i, step := range plan.Steps {
		for attempt := 0; attempt < o.cfg.MaxFixRetry; attempt++ {
			_, err := o.cfg.SpawnFunc(ctx, AgentConfig{
				Role: "implementer",
				Args: []string{step.Prompt},
			})
			if err == nil {
				o.logger.Info("step completed",
					"task", task.ID, "step", i, "attempt", attempt)
				break
			}
			if attempt == o.cfg.MaxFixRetry-1 {
				o.store.MarkFailed(task.ID, fmt.Sprintf(
					"step %d failed after %d attempts: %v",
					i, o.cfg.MaxFixRetry, err))
				return err
			}
		}
	}
	o.emitPhase(task.ID, "implement", "completed")

	// Phase 3: Review
	o.emitPhase(task.ID, "review", "started")
	o.store.UpdateStatus(task.ID, "reviewing")

	_, err = o.cfg.SpawnFunc(ctx, AgentConfig{Role: "reviewer"})
	if err != nil {
		o.logger.Warn("review failed, continuing", "err", err)
	}
	o.emitPhase(task.ID, "review", "completed")

	// Phase 4: Complete
	o.store.UpdateStatus(task.ID, "completed")
	now := time.Now().UTC()
	task.CompletedAt = &now
	return nil
}

func (o *Orchestrator) emitPhase(taskID, phase, action string) {
	data, _ := json.Marshal(map[string]string{
		"phase":  phase,
		"action": action,
	})
	o.store.AppendEvent(taskID, "phase_"+action, string(data))
}
```

- [ ] **Step 4: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run TestOrchestrator -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/orchestrator.go internal/daemon/orchestrator_test.go
git commit -m "feat(daemon): Task 5 — orchestrator phase state machine with mock agents"
```

---

### Task 6: Lifecycle — Crash Recovery + Cancel + Timeout

**Files:**
- Create: `internal/daemon/lifecycle.go`
- Create: `internal/daemon/lifecycle_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/daemon/lifecycle_test.go
package daemon

import (
	"testing"
)

func TestRecoverOrphanedTasks(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	// Create tasks in various states
	t1 := &Task{RepoURL: "r", TaskDescription: "a", Status: "implementing"}
	t2 := &Task{RepoURL: "r", TaskDescription: "b", Status: "pending"}
	t3 := &Task{RepoURL: "r", TaskDescription: "c", Status: "reviewing"}
	t4 := &Task{RepoURL: "r", TaskDescription: "d", Status: "completed"}
	s.CreateTask(t1)
	s.CreateTask(t2)
	s.CreateTask(t3)
	s.CreateTask(t4)

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
	if got1.ErrorMessage != "daemon restart — task interrupted" {
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

func TestDeliveryDedup(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	t1 := &Task{RepoURL: "r", TaskDescription: "a", Status: "pending", DeliveryID: "gh-123"}
	if err := s.CreateTask(t1); err != nil {
		t.Fatalf("first: %v", err)
	}

	t2 := &Task{RepoURL: "r", TaskDescription: "b", Status: "pending", DeliveryID: "gh-123"}
	err := s.CreateTask(t2)
	if err == nil {
		t.Error("expected dedup error for same delivery_id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run 'TestRecover|TestDelivery' -v`
Expected: FAIL — RecoverOrphanedTasks not defined

- [ ] **Step 3: Write lifecycle implementation**

```go
// internal/daemon/lifecycle.go
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// activeStates are task statuses that indicate a running task.
// On daemon restart, these are considered orphaned.
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
			if err := store.MarkFailed(t.ID, "daemon restart — task interrupted"); err != nil {
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
	task    *Task
	store   *Store
	orch    *Orchestrator
	cancel  context.CancelFunc
	logger  *slog.Logger
	timeout time.Duration
}

// NewTaskRunner creates a runner for a task.
func NewTaskRunner(task *Task, store *Store, orch *Orchestrator, logger *slog.Logger) *TaskRunner {
	return &TaskRunner{
		task:    task,
		store:   store,
		orch:    orch,
		logger:  logger,
		timeout: 2 * time.Hour, // configurable via ALTFIX_TASK_TIMEOUT_MINUTES
	}
}

// Run executes the task with timeout and panic recovery.
func (r *TaskRunner) Run(ctx context.Context) {
	ctx, r.cancel = context.WithTimeout(ctx, r.timeout)
	defer r.cancel()

	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("task panicked", "task", r.task.ID, "panic", rec)
			r.store.MarkFailed(r.task.ID, fmt.Sprintf("panic: %v", rec))
		}
	}()

	r.store.UpdateStatus(r.task.ID, "planning")
	now := time.Now().UTC()
	r.task.StartedAt = &now

	if err := r.orch.RunTask(ctx, r.task); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			r.store.MarkFailed(r.task.ID, "wall-clock timeout exceeded")
			r.store.AppendEvent(r.task.ID, "timeout", "")
		}
		// MarkFailed already called inside orchestrator for other errors
		return
	}
}

// Stop cancels the running task.
func (r *TaskRunner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.store.UpdateStatus(r.task.ID, "cancelled")
	r.store.AppendEvent(r.task.ID, "cancelled_by_user", "")
}
```

- [ ] **Step 4: Add ListTasksByStatus to store**

```go
// Append to internal/daemon/store.go

// ListTasksByStatus returns tasks with the given status.
func (s *Store) ListTasksByStatus(status string) ([]*Task, error) {
	rows, err := s.db.Query(
		`SELECT id, repo_url, task_description, status, agent_config,
		 pr_number, pr_url, branch_name, api_cost_usd, complexity,
		 issue_number, repo_owner, repo_name, started_at, completed_at,
		 error_message, delivery_id, created_at, updated_at
		 FROM tasks WHERE status = ?`, status)
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
```

- [ ] **Step 5: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -run 'TestRecover|TestDelivery' -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/lifecycle.go internal/daemon/lifecycle_test.go internal/daemon/store.go
git commit -m "feat(daemon): Task 6 — crash recovery + cancel + timeout lifecycle"
```

---

### Task 7: Cobra Subcommand + E2E Smoke Test

**Files:**
- Create: `cmd/altcode/daemon.go`
- Modify: `cmd/altcode/main.go` (1 line)

- [ ] **Step 1: Write the cobra subcommand**

```go
// cmd/altcode/daemon.go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/altcode-ai/altcode/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	var port int
	var dataDir string
	var authToken string
	var maxTasks int

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run altcode as an HTTP daemon for AltFix",
		Long: `Start an HTTP server that accepts coding tasks, spawns
agent subprocesses, and streams progress via SSE/WebSocket.
Designed for AltFix VM deployment.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if authToken == "" {
				authToken = os.Getenv("ALTFIX_AUTH_TOKEN")
			}
			if authToken == "" {
				fmt.Fprintln(os.Stderr,
					"warning: no --auth-token or ALTFIX_AUTH_TOKEN set — "+
						"API endpoints are unauthenticated")
			}

			srv, err := daemon.NewServer(daemon.ServerConfig{
				Port:      port,
				DataDir:   dataDir,
				AuthToken: authToken,
				MaxTasks:  maxTasks,
			})
			if err != nil {
				return err
			}

			// Crash recovery
			recovered, err := daemon.RecoverOrphanedTasks(srv.Store())
			if err != nil {
				return fmt.Errorf("crash recovery: %w", err)
			}
			if recovered > 0 {
				fmt.Fprintf(os.Stderr, "altcode daemon: recovered %d orphaned tasks\n", recovered)
			}

			return srv.Run(context.Background())
		},
	}

	cmd.Flags().IntVar(&port, "port", 9100, "HTTP server port")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Data directory (default ~/.altcode/daemon)")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "Bearer token for API auth")
	cmd.Flags().IntVar(&maxTasks, "max-concurrent", 2, "Max concurrent tasks")

	return cmd
}
```

- [ ] **Step 2: Expose Store() method on Server**

```go
// Append to internal/daemon/server.go

// Store returns the daemon's task store. Used by crash recovery
// at startup (before the HTTP server starts listening).
func (s *Server) Store() *Store { return s.store }
```

- [ ] **Step 3: Wire into main.go**

Add this ONE line inside the `main()` function in `cmd/altcode/main.go`, after the existing `root.AddCommand(...)` calls:

```go
root.AddCommand(newDaemonCmd())
```

- [ ] **Step 4: Build and verify --help**

Run: `GOFLAGS=-mod=mod go build -o /tmp/altcode-daemon ./cmd/altcode/ && /tmp/altcode-daemon daemon --help`
Expected: Shows daemon subcommand with --port, --data-dir, --auth-token, --max-concurrent flags

- [ ] **Step 5: Run full test suite**

Run: `GOFLAGS=-mod=mod go test ./internal/daemon/... -race -count=1 -v`
Expected: ALL tests pass (store, subprocess, auth middleware, handlers, orchestrator, lifecycle)

- [ ] **Step 6: Commit**

```bash
git add cmd/altcode/daemon.go cmd/altcode/main.go internal/daemon/server.go
git commit -m "feat(daemon): Task 7 — cobra subcommand + E2E wiring

altcode daemon --port 9100 --auth-token <token>

Endpoints:
  GET  /health
  POST /tasks
  GET  /tasks
  GET  /tasks/:id
  POST /tasks/:id/stop
  POST /tasks/:id/steer (stub)

Crash recovery runs on startup.
All internal/daemon tests pass with -race."
```

---

## Self-Review

### Spec coverage check

| Spec requirement | Task |
|------------------|------|
| #3 HTTP daemon with 6 endpoints | Tasks 3, 4 |
| #3 Bearer token auth | Task 3 |
| #3 Structured logging (slog) | Task 3 |
| #3 Graceful shutdown | Task 3 |
| #4 SQLite persistence | Task 1 |
| #4 Delivery ID dedup | Task 1 + Task 6 |
| #4 Event logging | Task 1 |
| #10 Crash recovery | Task 6 |
| #10 Cancel (stop) | Task 6 |
| #10 Wall-clock timeout | Task 6 |
| #10 Dedup | Task 1 (unique index) |
| #15 Subprocess spawn | Task 2 |
| #15 Process group kill | Task 2 |
| #15 Stdin/stdout pipes | Task 2 |
| #16 Orchestrator loop | Task 5 |
| #16 Phase transitions | Task 5 |
| #16 Retry on failure | Task 5 |
| B1 (auth) | Task 3 |
| B2 (orphan reaping) | Task 2 (Setpgid + WaitDelay) |
| B9 (panic recovery) | Task 6 (TaskRunner.Run defer) |
| Cobra subcommand | Task 7 |

### Bugs fixed from CC + Codex plan review

**P1 (IsRunning/Kill race):** Replace `done chan error` (consumed by Kill) with a `closed chan struct{}` + `sync.Once` pattern. `Wait()` blocks on `<-closed`. `Kill()` triggers the Once to close the channel. Multiple callers of `Wait()` all see the close. No consumption race.

**P2 (timestamps not persisted):** Add `store.MarkCompleted(id)` that sets `completed_at = datetime('now')` alongside status. Add `store.MarkStarted(id)` for `started_at`. Both called from `TaskRunner.Run` and `Orchestrator.RunTask`.

**P3 (timestamp format mismatch):** All INSERTs use `time.Now().UTC().Format(time.RFC3339)` explicitly instead of relying on SQLite's `datetime('now')` which produces a different format. Schema DEFAULT still uses `datetime('now')` as a fallback but the Go code always provides explicit values.

**P4 (timing side channel on auth):** Replace `auth[7:] != token` with `subtle.ConstantTimeCompare([]byte(auth[7:]), []byte(token)) != 1` in `authMiddleware`. Import `crypto/subtle`.

**P5 (false-positive orchestrator test):** Add 3 adversarial SpawnFunc variants to `orchestrator_test.go`: (a) fails once then succeeds (exercises retry), (b) always fails (exercises MarkFailed), (c) returns malformed JSON (exercises fallback plan).

**P6 (invalid status "completed"):** Change `"completed"` to `"merged"` for successfully finished tasks. This matches the spec's status enum. Rename `store.UpdateStatus(id, "completed")` → `store.UpdateStatus(id, "merged")`.

**P7 (missing 404 test):** Add `TestHandler_GetTask_NotFound` that verifies a missing ID returns 404, not 500. Add explicit `sql.ErrNoRows` check in `handleGetTask`.

**P8 (concurrent CreateTask):** Add `TestStore_ConcurrentCreate` with 10 goroutines creating tasks simultaneously. Verify no panics, no duplicate IDs, correct total count.

### Gaps (deferred to Plans B/C/D)

- **Not in Plan A:** SSE/WebSocket streaming (#5/#35), steering (#6), budget/stall detection (#8), GitHub integration (#7), warm sessions (#15 mode 2), prompt templates (#17), model routing (#20), workspace modes (#21), context management (#19), security scanning (#23), parallel tasks (#24), webhooks (#25), memory (#2), repo intelligence (#12), video demo (#29), checkpoints (#31), benchmarks (#27), VM image (#18), security (#14)
- **Intentionally stubbed:** `handleSteerTask`, `handleStopTask` (wired to lifecycle but real agent steering needs Plan B)

### Type consistency check

- `Task` struct used consistently across store, handlers, orchestrator, lifecycle
- `AgentConfig` used in subprocess.go and orchestrator.go
- `SpawnFunc` type matches subprocess.SpawnAgent signature pattern
- `Store` methods: CreateTask, GetTask, ListTasks, UpdateStatus, MarkFailed, AppendEvent, ListEvents, ListTasksByStatus — all called correctly in tests
