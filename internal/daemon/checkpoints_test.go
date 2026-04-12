package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckpoint_CreateAndList(t *testing.T) {
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

	cp := &Checkpoint{
		TaskID:       task.ID,
		Phase:        "plan",
		PhaseNumber:  1,
		GitSHA:       "abc123",
		TestSummary:  "3/3 passed",
		CostSoFar:    0.42,
		FilesChanged: 5,
	}
	if err := s.CreateCheckpoint(cp); err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if cp.ID == "" {
		t.Error("expected non-empty checkpoint ID")
	}

	cps, err := s.ListCheckpoints(task.ID)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 1 {
		t.Fatalf("got %d checkpoints, want 1", len(cps))
	}
	if cps[0].Phase != "plan" {
		t.Errorf("phase = %q, want plan", cps[0].Phase)
	}
	if cps[0].GitSHA != "abc123" {
		t.Errorf("git_sha = %q, want abc123", cps[0].GitSHA)
	}
}

func TestCheckpoint_GetSingle(t *testing.T) {
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

	cp := &Checkpoint{
		TaskID:      task.ID,
		Phase:       "implement",
		PhaseNumber: 2,
		GitSHA:      "def456",
	}
	if err := s.CreateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetCheckpoint(cp.ID)
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if got.Phase != "implement" {
		t.Errorf("phase = %q, want implement", got.Phase)
	}
	if got.TaskID != task.ID {
		t.Errorf("task_id = %q, want %q", got.TaskID, task.ID)
	}
}

func TestHandler_ListCheckpoints(t *testing.T) {
	srv := testServer(t)
	taskID := createTestTask(t, srv)

	// Create a checkpoint directly.
	cp := &Checkpoint{
		TaskID:       taskID,
		Phase:        "plan",
		PhaseNumber:  1,
		GitSHA:       "aaa111",
		TestSummary:  "ok",
		CostSoFar:    0.10,
		FilesChanged: 2,
	}
	if err := srv.store.CreateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/tasks/"+taskID+"/checkpoints", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("list checkpoints: got %d, body: %s",
			rec.Code, rec.Body.String())
	}
	var cps []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(cps) != 1 {
		t.Errorf("got %d checkpoints, want 1", len(cps))
	}
}

func TestHandler_RestoreCheckpoint_Returns202(t *testing.T) {
	srv := testServer(t)
	taskID := createTestTask(t, srv)

	cp := &Checkpoint{
		TaskID:      taskID,
		Phase:       "implement",
		PhaseNumber: 2,
		GitSHA:      "bbb222",
	}
	if err := srv.store.CreateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}

	body := `{"checkpoint_id":"` + cp.ID + `"}`
	req := httptest.NewRequest("POST", "/tasks/"+taskID+"/restore",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("restore: got %d, body: %s",
			rec.Code, rec.Body.String())
	}

	// Verify restore_requested event was logged.
	events, err := srv.store.ListEvents(taskID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.EventType == "restore_requested" {
			found = true
		}
	}
	if !found {
		t.Error("expected restore_requested event")
	}
}

func TestHandler_RestoreCheckpoint_WrongTask(t *testing.T) {
	srv := testServer(t)
	taskID1 := createTestTask(t, srv)
	taskID2 := createTestTask(t, srv)

	cp := &Checkpoint{
		TaskID:      taskID1,
		Phase:       "plan",
		PhaseNumber: 1,
		GitSHA:      "ccc333",
	}
	if err := srv.store.CreateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}

	// Try to restore checkpoint belonging to task1 on task2.
	body := `{"checkpoint_id":"` + cp.ID + `"}`
	req := httptest.NewRequest("POST", "/tasks/"+taskID2+"/restore",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for wrong task, got %d", rec.Code)
	}
}

func TestHandler_ListCheckpoints_TaskNotFound(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET",
		"/tasks/nonexistent/checkpoints", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404 for missing task, got %d", rec.Code)
	}
}
