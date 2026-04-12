package daemon

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(ServerConfig{
		Port:      0,
		DataDir:   t.TempDir(),
		AuthToken: "test",
	})
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
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal health: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("health body: %v", body)
	}
}

func TestHandler_CreateAndGetTask(t *testing.T) {
	s := testServer(t)

	// Create.
	payload := `{"repo_url":"https://github.com/t/r","task":"fix bug"}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("create: got %d, body: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	taskID, ok := created["id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("expected task ID, got: %v", created)
	}

	// Get.
	req2 := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	rec2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rec2, req2)

	if rec2.Code != 200 {
		t.Errorf("get: got %d, body: %s", rec2.Code, rec2.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get: %v", err)
	}
	taskObj, ok := got["task"].(map[string]any)
	if !ok {
		t.Fatalf("expected task object in response, got: %v", got)
	}
	if taskObj["id"] != taskID {
		t.Errorf("get id = %v, want %s", taskObj["id"], taskID)
	}
}

func TestHandler_ListTasks(t *testing.T) {
	s := testServer(t)

	// Create 2 tasks.
	for _, desc := range []string{"a", "b"} {
		payload := `{"repo_url":"r","task":"` + desc + `"}`
		req := httptest.NewRequest("POST", "/tasks",
			strings.NewReader(payload))
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		if rec.Code != 201 {
			t.Fatalf("create %q: got %d", desc, rec.Code)
		}
	}

	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("list: got %d", rec.Code)
	}
	var tasks []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestHandler_GetTask_NotFound(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/tasks/nonexistent-id", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404 for missing task, got %d", rec.Code)
	}
}

func TestHandler_CreateTask_InvalidJSON(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/tasks",
		strings.NewReader("{bad json"))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestHandler_CreateTask_MissingFields(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/tasks",
		strings.NewReader(`{"repo_url":"r"}`))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for missing task field, got %d", rec.Code)
	}
}

func TestHandler_CreateTask_MissingRepoURL(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/tasks",
		strings.NewReader(`{"task":"do something"}`))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for missing repo_url, got %d", rec.Code)
	}
}

func TestHandler_StopTask_Stub(t *testing.T) {
	s := testServer(t)

	// Create a task first.
	payload := `{"repo_url":"https://github.com/t/r","task":"fix bug"}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create: got %d", rec.Code)
	}
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	taskID := created["id"].(string)

	// Stop.
	req2 := httptest.NewRequest("POST", "/tasks/"+taskID+"/stop", nil)
	rec2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rec2, req2)

	if rec2.Code != 202 {
		t.Errorf("expected 202 for stop stub, got %d", rec2.Code)
	}
	ct := rec2.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}
}

func createTestTask(t *testing.T, s *Server) string {
	t.Helper()
	payload := `{"repo_url":"https://github.com/t/r","task":"fix bug"}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create: got %d, body: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	return created["id"].(string)
}

func TestHandler_SteerTask_WithMessage(t *testing.T) {
	s := testServer(t)
	taskID := createTestTask(t, s)

	body := `{"message":"focus on error handling"}`
	req := httptest.NewRequest("POST", "/tasks/"+taskID+"/steer",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Errorf("expected 202, got %d, body: %s",
			rec.Code, rec.Body.String())
	}

	// Verify event was logged.
	events, err := s.store.ListEvents(taskID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	found := false
	for _, e := range events {
		if e.EventType == "user_steer" {
			found = true
		}
	}
	if !found {
		t.Error("expected user_steer event, not found")
	}
}

func TestHandler_SteerTask_MissingMessage(t *testing.T) {
	s := testServer(t)
	taskID := createTestTask(t, s)

	// Empty body.
	req := httptest.NewRequest("POST", "/tasks/"+taskID+"/steer",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400 for missing message, got %d", rec.Code)
	}
}

func TestHandler_SteerTask_Completed(t *testing.T) {
	s := testServer(t)
	taskID := createTestTask(t, s)

	// Mark task as merged (terminal state).
	if err := s.store.MarkCompleted(taskID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	body := `{"message":"too late"}`
	req := httptest.NewRequest("POST", "/tasks/"+taskID+"/steer",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Errorf("expected 409 for completed task, got %d", rec.Code)
	}
}

func TestHandler_StopTask_NotFound(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("POST", "/tasks/nonexistent-id/stop", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 404 {
		t.Errorf("expected 404 for missing task, got %d", rec.Code)
	}
}

func TestHandler_StopTask_AlreadyDone(t *testing.T) {
	s := testServer(t)
	taskID := createTestTask(t, s)

	// Mark task as merged (terminal state).
	if err := s.store.MarkCompleted(taskID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	req := httptest.NewRequest("POST", "/tasks/"+taskID+"/stop", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 409 {
		t.Errorf("expected 409 for terminal task, got %d", rec.Code)
	}
}

func TestHandler_CreateTask_WhitespaceFields(t *testing.T) {
	s := testServer(t)

	tests := []struct {
		name    string
		payload string
	}{
		{"whitespace repo_url", `{"repo_url":"   ","task":"fix bug"}`},
		{"whitespace task", `{"repo_url":"https://github.com/t/r","task":"  "}`},
		{"tabs and spaces", `{"repo_url":"\t  ","task":"\t  "}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/tasks",
				strings.NewReader(tt.payload))
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)

			if rec.Code != 400 {
				t.Errorf("expected 400, got %d, body: %s",
					rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandler_QueuePosition_PendingTask(t *testing.T) {
	s := testServer(t)

	// Create 3 pending tasks.
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = createTestTask(t, s)
	}

	// The third task should have position > 0 (queued behind others).
	req := httptest.NewRequest("GET", "/tasks/"+ids[2], nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("get: got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	q, ok := resp["queue"].(map[string]any)
	if !ok {
		t.Fatalf("expected queue info, got: %v", resp)
	}
	pos := int(q["queue_position"].(float64))
	if pos <= 0 {
		t.Errorf("expected queue_position > 0 for third pending task, got %d", pos)
	}
}

func TestHandler_QueuePosition_RunningTask(t *testing.T) {
	s := testServer(t)
	taskID := createTestTask(t, s)

	// Transition to a non-pending status.
	if err := s.store.UpdateStatus(taskID, "implementing"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/tasks/"+taskID, nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("get: got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	q, ok := resp["queue"].(map[string]any)
	if !ok {
		t.Fatalf("expected queue info, got: %v", resp)
	}
	pos := int(q["queue_position"].(float64))
	if pos != 0 {
		t.Errorf("expected queue_position=0 for running task, got %d", pos)
	}
}
