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
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "fix",
		Status:          "pending",
	}
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

	s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	})
	s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "b", Status: "pending",
	})

	tasks, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestStore_ListTasksByStatus(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	})
	s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "b", Status: "implementing",
	})
	s.CreateTask(&Task{
		RepoURL: "r", TaskDescription: "c", Status: "pending",
	})

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
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}
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

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "implementing",
	}
	s.CreateTask(task)
	s.MarkFailed(task.ID, "timeout")

	got, _ := s.GetTask(task.ID)
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
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pr_open",
	}
	s.CreateTask(task)
	s.MarkCompleted(task.ID)

	got, _ := s.GetTask(task.ID)
	if got.Status != "merged" {
		t.Errorf("status = %q, want merged", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestStore_MarkStarted(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}
	s.CreateTask(task)

	if task.StartedAt != nil {
		t.Error("expected started_at to be nil before MarkStarted")
	}

	s.MarkStarted(task.ID)

	got, _ := s.GetTask(task.ID)
	if got.StartedAt == nil {
		t.Error("expected started_at to be set after MarkStarted")
	}
}

func TestStore_AppendAndListEvents(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	task := &Task{
		RepoURL: "r", TaskDescription: "a", Status: "pending",
	}
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

	// Test afterID filtering.
	events2, _ := s.ListEvents(task.ID, events[0].ID)
	if len(events2) != 1 {
		t.Errorf("filtered events: got %d, want 1", len(events2))
	}
}

func TestStore_DeliveryIDDedup(t *testing.T) {
	s, _ := NewStore(":memory:")
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
	err := s.CreateTask(t2)
	if err == nil {
		t.Error("expected duplicate delivery_id to fail")
	}
}

func TestStore_ConcurrentCreate(t *testing.T) {
	s, _ := NewStore(":memory:")
	defer s.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tk := &Task{
				RepoURL:         "r",
				TaskDescription: fmt.Sprintf("task-%d", n),
				Status:          "pending",
			}
			s.CreateTask(tk)
		}(i)
	}
	wg.Wait()

	tasks, _ := s.ListTasks()
	if len(tasks) != 10 {
		t.Errorf("got %d tasks, want 10", len(tasks))
	}
}
