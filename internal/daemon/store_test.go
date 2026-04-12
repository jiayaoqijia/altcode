package daemon

import (
	"fmt"
	"sync"
	"testing"
)

func TestNewStore_CreatesSchema(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer s.Close()

	// Schema should exist — verify by inserting a task.
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

func TestStore_GetTask(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "fix",
		Status:          "pending",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TaskDescription != "fix" {
		t.Errorf("description = %q, want fix", got.TaskDescription)
	}
}

func TestStore_ListTasks(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "b", Status: "pending",
	}); err != nil {
		t.Fatal(err)
	}

	tasks, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestStore_ListTasksByStatus(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "b", Status: "implementing",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "c", Status: "pending",
	}); err != nil {
		t.Fatal(err)
	}

	pending, err := s.ListTasksByStatus("pending")
	if err != nil {
		t.Fatalf("ListTasksByStatus: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("got %d pending tasks, want 2", len(pending))
	}

	impl, err := s.ListTasksByStatus("implementing")
	if err != nil {
		t.Fatalf("ListTasksByStatus: %v", err)
	}
	if len(impl) != 1 {
		t.Errorf("got %d implementing tasks, want 1", len(impl))
	}

	empty, err := s.ListTasksByStatus("merged")
	if err != nil {
		t.Fatalf("ListTasksByStatus: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d merged tasks, want 0", len(empty))
	}
}

func TestStore_UpdateStatus(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateStatus(task.ID, "implementing"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "implementing" {
		t.Errorf("status = %q, want implementing", got.Status)
	}
}

func TestStore_MarkFailed(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "implementing",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFailed(task.ID, "timeout"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.ErrorMessage != "timeout" {
		t.Errorf("error = %q, want timeout", got.ErrorMessage)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestStore_MarkCompleted(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pr_open",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCompleted(task.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want merged", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestStore_MarkStarted(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	if task.StartedAt != nil {
		t.Error("expected started_at to be nil before MarkStarted")
	}

	if err := s.MarkStarted(task.ID); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.StartedAt == nil {
		t.Error("expected started_at to be set after MarkStarted")
	}
}

func TestStore_AppendAndListEvents(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	if err := s.AppendEvent(task.ID, "phase_started", `{"phase":"plan"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, "phase_completed", `{"phase":"plan"}`); err != nil {
		t.Fatal(err)
	}

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

	// Test afterID filtering.
	events2, err := s.ListEvents(task.ID, events[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events2) != 1 {
		t.Errorf("filtered events: got %d, want 1", len(events2))
	}
}

func TestStore_DeliveryIDDedup(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	t1 := &Task{
		RepoURL: "r", TaskDescription: "a",
		Status: "pending", DeliveryID: "gh-abc",
	}
	if err := s.CreateTask(t1); err != nil {
		t.Fatalf("first create: %v", err)
	}
	t2 := &Task{
		RepoURL: "r", TaskDescription: "b",
		Status: "pending", DeliveryID: "gh-abc",
	}
	err = s.CreateTask(t2)
	if err == nil {
		t.Error("expected duplicate delivery_id to fail")
	}
}

func TestStore_UpdateStatus_NonexistentID(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.UpdateStatus("nonexistent-id", "implementing")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestStore_MarkFailed_NonexistentID(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.MarkFailed("nonexistent-id", "some error")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestStore_MarkCompleted_NonexistentID(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.MarkCompleted("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestStore_MarkStarted_NonexistentID(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.MarkStarted("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestStore_GetTask_NonexistentID(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, err = s.GetTask("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent ID")
	}
}

func TestStore_GetTask_EmptyID(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, err = s.GetTask("")
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestStore_ListEvents_NonexistentTaskID(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	events, err := s.ListEvents("nonexistent-task", 0)
	if err != nil {
		t.Fatalf("ListEvents should not error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestStore_CreateTask_EmptyOptionalFields(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "minimal task",
		Status:          "pending",
	}
	if err := s.CreateTask(task); err != nil {
		t.Fatalf("CreateTask with empty optionals: %v", err)
	}
	if task.ID == "" {
		t.Error("expected non-empty ID")
	}

	got, err := s.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentConfig != "" {
		t.Errorf("expected empty AgentConfig, got %q", got.AgentConfig)
	}
	if got.BranchName != "" {
		t.Errorf("expected empty BranchName, got %q", got.BranchName)
	}
	if got.Complexity != "" {
		t.Errorf("expected empty Complexity, got %q", got.Complexity)
	}
	if got.DeliveryID != "" {
		t.Errorf("expected empty DeliveryID, got %q", got.DeliveryID)
	}
	if got.PRNumber != 0 {
		t.Errorf("expected 0 PRNumber, got %d", got.PRNumber)
	}
	if got.IssueNumber != 0 {
		t.Errorf("expected 0 IssueNumber, got %d", got.IssueNumber)
	}
}

func TestStore_NewStore_InvalidPath(t *testing.T) {
	_, err := NewStore("/nonexistent/path/db.sqlite")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestStore_ConcurrentCreate(t *testing.T) {
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tk := &Task{
				RepoURL:         "r",
				TaskDescription: fmt.Sprintf("task-%d", n),
				Status:          "pending",
			}
			if err := s.CreateTask(tk); err != nil {
				errCh <- fmt.Errorf("task-%d: %w", n, err)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent create error: %v", err)
	}

	tasks, err := s.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 10 {
		t.Errorf("got %d tasks, want 10", len(tasks))
	}
}
