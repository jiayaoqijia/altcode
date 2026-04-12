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
	if got["ID"] != taskID {
		t.Errorf("get ID = %v, want %s", got["ID"], taskID)
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
