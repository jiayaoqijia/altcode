package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockEventStore implements EventStore for testing.
type mockEventStore struct {
	task   *TaskView
	events []*EventView
	err    error
}

func (m *mockEventStore) GetTask(id string) (*TaskView, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.task == nil || m.task.ID != id {
		return nil, fmt.Errorf("not found")
	}
	return m.task, nil
}

func (m *mockEventStore) ListEvents(
	taskID string, afterID int64,
) ([]*EventView, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []*EventView
	for _, ev := range m.events {
		if ev.TaskID == taskID && ev.ID > afterID {
			out = append(out, ev)
		}
	}
	return out, nil
}

func TestRenderEventHTML_AllTypes(t *testing.T) {
	types := []struct {
		eventType string
		wantClass string
	}{
		{"phase_started", "feed-item-phase"},
		{"phase_completed", "feed-item-phase"},
		{"agent_output", "feed-item-agent"},
		{"tool_call", "feed-item-agent"},
		{"error", "feed-item-error"},
		{"user_steer", "feed-item-steer"},
		{"pr_created", "feed-item-info"},
		{"ci_status", "feed-item-info"},
		{"unknown_type", "feed-item"},
	}
	for _, tt := range types {
		ev := &EventView{
			ID:        1,
			TaskID:    "t1",
			EventType: tt.eventType,
			Data:      "test data",
			CreatedAt: time.Now(),
		}
		html, err := renderEventHTML(ev)
		if err != nil {
			t.Errorf("renderEventHTML(%s): %v", tt.eventType, err)
			continue
		}
		if !strings.Contains(html, tt.wantClass) {
			t.Errorf("renderEventHTML(%s) missing class %q\ngot: %s",
				tt.eventType, tt.wantClass, html)
		}
		if !strings.Contains(html, `data-event-id="1"`) {
			t.Errorf("renderEventHTML(%s) missing data-event-id",
				tt.eventType)
		}
		if !strings.Contains(html, "test data") {
			t.Errorf("renderEventHTML(%s) missing data content",
				tt.eventType)
		}
	}
}

func TestRenderEventHTML_ErrorType(t *testing.T) {
	ev := &EventView{
		ID:        42,
		TaskID:    "t1",
		EventType: "error",
		Data:      "something broke",
		CreatedAt: time.Now(),
	}
	html, err := renderEventHTML(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "ef4444") {
		t.Error("error event should use red color")
	}
	if !strings.Contains(html, "something broke") {
		t.Error("error event should contain data")
	}
}

func TestIsTerminalStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"merged", true},
		{"closed", true},
		{"failed", true},
		{"cancelled", true},
		{"pending", false},
		{"implementing", false},
		{"planning", false},
		{"testing", false},
	}
	for _, tt := range tests {
		got := isTerminalStatus(tt.status)
		if got != tt.want {
			t.Errorf("isTerminalStatus(%q) = %v, want %v",
				tt.status, got, tt.want)
		}
	}
}

func TestHandleSSEHTML_NotFound(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	store := &mockEventStore{task: nil, err: fmt.Errorf("not found")}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/{id}/events", h.HandleSSEHTML)

	req := httptest.NewRequest("GET", "/ui/tasks/nonexistent/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandleSSEHTML_NoStore(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	// Pass a non-EventStore value.
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, "not-an-event-store", sessions, WebConfig{}, NewOrgCache(time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/{id}/events", h.HandleSSEHTML)

	req := httptest.NewRequest("GET", "/ui/tasks/t1/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}

func TestHandleSSEHTML_StreamsAndStops(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	store := &mockEventStore{
		task: &TaskView{
			ID:     "t1",
			Status: "merged", // terminal
		},
		events: []*EventView{
			{
				ID:        1,
				TaskID:    "t1",
				EventType: "phase_started",
				Data:      "planning",
				CreatedAt: time.Now(),
			},
		},
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/{id}/events", h.HandleSSEHTML)

	// Use a cancellable context with a short timeout.
	ctx, cancel := context.WithTimeout(
		context.Background(), 5*time.Second,
	)
	defer cancel()

	req := httptest.NewRequest("GET", "/ui/tasks/t1/events", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream content type, got %q", ct)
	}
	if !strings.Contains(body, "id: 1") {
		t.Error("expected event id in SSE stream")
	}
	if !strings.Contains(body, "feed-item-phase") {
		t.Error("expected rendered HTML partial in stream")
	}
	if !strings.Contains(body, "heartbeat") {
		t.Error("expected heartbeat in SSE stream")
	}
}

func TestHandleTaskDetail_NotFound(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	store := &mockEventStore{
		task: nil,
		err:  fmt.Errorf("not found"),
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/{id}", h.HandleTaskDetail)

	req := httptest.NewRequest("GET", "/ui/tasks/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestHandleTaskDetail_Renders(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store := &mockEventStore{
		task: &TaskView{
			ID:              "task-abc",
			RepoOwner:       "octocat",
			RepoName:        "hello",
			IssueNumber:     42,
			TaskDescription: "Fix the bug",
			Status:          "implementing",
			APICostUSD:      1.23,
			PRURL:           "https://github.com/octocat/hello/pull/99",
			PRNumber:        99,
			CreatedAt:       now,
		},
		events: []*EventView{
			{
				ID:        1,
				TaskID:    "task-abc",
				EventType: "phase_started",
				Data:      "planning",
				CreatedAt: now,
			},
		},
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "octocat"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/{id}", h.HandleTaskDetail)

	req := httptest.NewRequest("GET", "/ui/tasks/task-abc", nil)
	// Inject session into context.
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	checks := []string{
		"octocat/hello#42",
		"Fix the bug",
		"implementing",
		"$1.23",
		"PR #99",
		"Cancel",                    // active task shows cancel
		"activity-feed",             // feed container
		"/ui/tasks/task-abc/events", // EventSource URL
		"Steer the agent",           // steering form
		"Dashboard",                 // back link
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
}

func TestHandleTaskDetail_TerminalHidesSteering(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	store := &mockEventStore{
		task: &TaskView{
			ID:     "task-done",
			Status: "merged",
			CreatedAt: time.Now(),
		},
		events: nil,
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "user"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/{id}", h.HandleTaskDetail)

	req := httptest.NewRequest("GET", "/ui/tasks/task-done", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "Steer the agent") {
		t.Error("merged task should not show steering form")
	}
	if strings.Contains(body, "Cancel") {
		t.Error("merged task should not show Cancel button")
	}
}
