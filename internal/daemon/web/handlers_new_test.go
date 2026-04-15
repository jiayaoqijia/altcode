package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockFullStore implements StoreIface, RepoStore, and IssueStore
// for testing the new handlers.
type mockFullStore struct {
	tasks  []*TaskView
	repos  []string
	issues map[string][]*IssueView
}

func (m *mockFullStore) ListTasks() ([]*TaskView, error) {
	return m.tasks, nil
}

func (m *mockFullStore) GetTask(id string) (*TaskView, error) {
	for _, t := range m.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockFullStore) ListEvents(
	_ string, _ int64,
) ([]*EventView, error) {
	return nil, nil
}

func (m *mockFullStore) ListRepos() ([]string, error) {
	return m.repos, nil
}

func (m *mockFullStore) ListIssues(repo string) ([]*IssueView, error) {
	return m.issues[repo], nil
}

func TestHandleNewTask_Renders(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	store := &mockFullStore{
		repos: []string{"octocat/hello", "acme/api"},
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "octocat"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/new", h.HandleNewTask)

	req := httptest.NewRequest("GET", "/ui/tasks/new", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	checks := []string{
		"New Task",
		"octocat/hello",
		"acme/api",
		"Repository",
		"Task Description",
		"Advanced Options",
		"Model",
		"Max Cost",
		"Max Turns",
		"Create Task",
		"Dashboard",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("new task page missing %q", want)
		}
	}
}

func TestHandleNewTask_NoRepos(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	store := &mockFullStore{repos: nil}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "user"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/new", h.HandleNewTask)

	req := httptest.NewRequest("GET", "/ui/tasks/new", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "No repositories available") {
		t.Error("expected empty-state message for no repos")
	}
}

func TestHandleNewTask_NoStore(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "user"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, nil, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/tasks/new", h.HandleNewTask)

	req := httptest.NewRequest("GET", "/ui/tasks/new", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "No repositories available") {
		t.Error("nil store should show empty repos state")
	}
}

func TestHandlePartialRepoIssues_WithIssues(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	store := &mockFullStore{
		issues: map[string][]*IssueView{
			"octocat/hello": {
				{Number: 1, Title: "Fix bug"},
				{Number: 2, Title: "Add feature"},
			},
		},
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /ui/partials/repo-issues",
		h.HandlePartialRepoIssues,
	)

	req := httptest.NewRequest(
		"GET", "/ui/partials/repo-issues?repo=octocat/hello", nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Fix bug") {
		t.Error("expected issue title in partial")
	}
	if !strings.Contains(body, "#1") {
		t.Error("expected issue number in partial")
	}
	if !strings.Contains(body, "#2") {
		t.Error("expected second issue number")
	}
}

func TestHandlePartialRepoIssues_NoRepo(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	store := &mockFullStore{}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /ui/partials/repo-issues",
		h.HandlePartialRepoIssues,
	)

	req := httptest.NewRequest(
		"GET", "/ui/partials/repo-issues", nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Task Description") {
		t.Error("expected description textarea in partial")
	}
}

func TestHandlePRs_Renders(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store := &mockFullStore{
		tasks: []*TaskView{
			{
				ID:              "t1",
				RepoOwner:       "octocat",
				RepoName:        "hello",
				PRNumber:        42,
				PRURL:           "https://github.com/octocat/hello/pull/42",
				TaskDescription: "Fix the login bug",
				Status:          "merged",
				APICostUSD:      2.50,
				CreatedAt:       now,
			},
			{
				ID:              "t2",
				RepoOwner:       "acme",
				RepoName:        "api",
				PRNumber:        10,
				PRURL:           "https://github.com/acme/api/pull/10",
				TaskDescription: "Add endpoint",
				Status:          "pr_open",
				APICostUSD:      1.00,
				CreatedAt:       now,
			},
			{
				ID:              "t3",
				RepoOwner:       "acme",
				RepoName:        "cli",
				TaskDescription: "No PR yet",
				Status:          "implementing",
				CreatedAt:       now,
			},
		},
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "user"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/prs", h.HandlePRs)

	req := httptest.NewRequest("GET", "/ui/prs", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	checks := []string{
		"PR Tracker",
		"octocat/hello",
		"#42",
		"Fix the login bug",
		"$2.50",
		"acme/api",
		"#10",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("PR page missing %q", want)
		}
	}
	// Task without PR should not appear.
	if strings.Contains(body, "acme/cli") {
		t.Error("task without PR should not appear in PR tracker")
	}
}

func TestHandlePRs_FilterByStatus(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store := &mockFullStore{
		tasks: []*TaskView{
			{
				ID:        "t1",
				RepoOwner: "a",
				RepoName:  "b",
				PRNumber:  1,
				PRURL:     "https://github.com/a/b/pull/1",
				Status:    "merged",
				CreatedAt: now,
			},
			{
				ID:        "t2",
				RepoOwner: "c",
				RepoName:  "d",
				PRNumber:  2,
				PRURL:     "https://github.com/c/d/pull/2",
				Status:    "pr_open",
				CreatedAt: now,
			},
		},
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "user"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/prs", h.HandlePRs)

	req := httptest.NewRequest("GET", "/ui/prs?status=merged", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "#1") {
		t.Error("expected merged PR to appear")
	}
	if strings.Contains(body, "#2") {
		t.Error("open PR should be filtered out with status=merged")
	}
}

func TestHandlePRs_EmptyState(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	store := &mockFullStore{tasks: nil}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{Login: "user"})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/prs", h.HandlePRs)

	req := httptest.NewRequest("GET", "/ui/prs", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "No PRs created yet") {
		t.Error("expected empty state message")
	}
}

func TestHandleSettings_AdminRenders(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{
		Login:     "admin1",
		AvatarURL: "https://example.com/avatar.png",
		IsAdmin:   true,
		Orgs:      []string{"myorg", "other"},
	})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, nil, sessions,
		WebConfig{
			AllowedUsers: []string{"admin1", "user2"},
			AllowedOrgs:  []string{"myorg"},
			AdminUsers:   []string{"admin1"},
		},
		NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/settings", h.HandleSettings)

	req := httptest.NewRequest("GET", "/ui/settings", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	checks := []string{
		"Settings",
		"Connected GitHub",
		"admin1",
		"Budget",
		"Daily Cap",
		"Per-Task Cap",
		"Max Concurrent",
		"Model Routing",
		"Access Control",
		"Allowed Users",
		"user2",
		"Allowed Orgs",
		"myorg",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
	// Admin should NOT see the read-only warning.
	if strings.Contains(body, "read-only access") {
		t.Error("admin should not see read-only warning")
	}
}

func TestHandleSettings_NonAdminReadOnly(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewSessionStore(time.Hour)
	sid := sessions.Create(&SessionUser{
		Login:   "viewer",
		IsAdmin: false,
	})
	sess, _ := sessions.Get(sid)
	h := NewWebHandler(
		tmpl, nil, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/settings", h.HandleSettings)

	req := httptest.NewRequest("GET", "/ui/settings", nil)
	ctx := withSession(req.Context(), sess)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "read-only access") {
		t.Error("non-admin should see read-only warning")
	}
}

func TestHandleSettings_NoSession(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	h := NewWebHandler(
		tmpl, nil,
		NewSessionStore(time.Hour),
		WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui/settings", h.HandleSettings)

	req := httptest.NewRequest("GET", "/ui/settings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
}

func TestExtractPRs(t *testing.T) {
	now := time.Now()
	tasks := []*TaskView{
		{
			ID: "t1", PRURL: "https://example.com/1",
			PRNumber: 1, Status: "merged", CreatedAt: now,
		},
		{
			ID: "t2", PRURL: "https://example.com/2",
			PRNumber: 2, Status: "pr_open", CreatedAt: now,
		},
		{
			ID: "t3", PRURL: "",
			Status: "implementing", CreatedAt: now,
		},
	}

	// No filter: only tasks with PRs.
	prs := extractPRs(tasks, "")
	if len(prs) != 2 {
		t.Errorf("extractPRs no filter: got %d, want 2", len(prs))
	}

	// Filter by merged.
	prs = extractPRs(tasks, "merged")
	if len(prs) != 1 {
		t.Errorf("extractPRs merged: got %d, want 1", len(prs))
	}
	if prs[0].PRNumber != 1 {
		t.Errorf("expected PR #1, got #%d", prs[0].PRNumber)
	}

	// Filter by nonexistent status.
	prs = extractPRs(tasks, "draft")
	if len(prs) != 0 {
		t.Errorf("extractPRs draft: got %d, want 0", len(prs))
	}
}

func TestPRStatusMatch(t *testing.T) {
	tests := []struct {
		status string
		filter string
		want   bool
	}{
		{"merged", "", true},
		{"merged", "merged", true},
		{"merged", "MERGED", true},
		{"open", "merged", false},
		{"pr_open", "pr_open", true},
	}
	for _, tt := range tests {
		got := prStatusMatch(tt.status, tt.filter)
		if got != tt.want {
			t.Errorf("prStatusMatch(%q, %q) = %v, want %v",
				tt.status, tt.filter, got, tt.want)
		}
	}
}

func TestHandlePartialPRList(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	store := &mockFullStore{
		tasks: []*TaskView{
			{
				ID: "t1", RepoOwner: "a", RepoName: "b",
				PRNumber: 5, PRURL: "https://github.com/a/b/pull/5",
				Status: "merged", APICostUSD: 1.0, CreatedAt: now,
			},
		},
	}
	sessions := NewSessionStore(time.Hour)
	h := NewWebHandler(
		tmpl, store, sessions, WebConfig{}, NewOrgCache(time.Hour),
	)

	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /ui/partials/pr-list",
		h.HandlePartialPRList,
	)

	req := httptest.NewRequest("GET", "/ui/partials/pr-list", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "#5") {
		t.Error("expected PR #5 in partial")
	}
	if !strings.Contains(body, "a/b") {
		t.Error("expected repo name in partial")
	}
}

func TestLoadTemplates_NewPages(t *testing.T) {
	tmpl, err := LoadTemplates("")
	if err != nil {
		t.Fatal(err)
	}
	pages := []string{"new", "prs", "settings"}
	for _, name := range pages {
		if tmpl.Page(name) == nil {
			t.Errorf("expected %q page template to be loaded", name)
		}
	}
}
