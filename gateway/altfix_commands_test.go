package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAltFixBridge_GetTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/tasks/abc123" {
				t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"id":               "abc12345deadbeef",
					"task_description": "fix login",
					"status":           "implementing",
					"api_cost_usd":     0.12,
					"pr_url":           "https://github.com/o/r/pull/7",
					"error_message":    "",
					"created_at":       "2026-04-12T10:00:00Z",
				},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.getTask(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"abc12345", "implementing", "fix login", "$0.1200",
		"PR: https://github.com/o/r/pull/7",
		"Created: 2026-04-12T10:00:00Z",
	} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply missing %q: %s", want, reply)
		}
	}
	// Error should be absent when empty.
	if strings.Contains(reply, "Error:") {
		t.Errorf("reply should not show Error: when empty: %s", reply)
	}
}

func TestAltFixBridge_GetTaskWithError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"id":            "fail1234",
					"status":        "failed",
					"error_message": "OOM killed",
				},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.getTask(context.Background(), "fail1234")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Error: OOM killed") {
		t.Errorf("reply should show error: %s", reply)
	}
}

func TestAltFixBridge_ListTasksByStatus(t *testing.T) {
	tasks := []map[string]any{
		{"id": "t1", "task_description": "a", "status": "implementing", "api_cost_usd": 0.01},
		{"id": "t2", "task_description": "b", "status": "failed", "api_cost_usd": 0.02},
		{"id": "t3", "task_description": "c", "status": "merged", "api_cost_usd": 0.03},
		{"id": "t4", "task_description": "d", "status": "pending", "api_cost_usd": 0.00},
		{"id": "t5", "task_description": "e", "status": "closed", "api_cost_usd": 0.05},
	}

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(tasks)
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	tests := []struct {
		filter  string
		wantIDs []string
		wantNot []string
	}{
		{"active", []string{"t1", "t4"}, []string{"t2", "t3", "t5"}},
		{"failed", []string{"t2"}, []string{"t1", "t3"}},
		{"completed", []string{"t3", "t5"}, []string{"t1", "t2"}},
	}

	for _, tt := range tests {
		t.Run(tt.filter, func(t *testing.T) {
			reply, err := bridge.listTasksByStatus(
				context.Background(), tt.filter,
			)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range tt.wantIDs {
				if !strings.Contains(reply, id) {
					t.Errorf("filter=%s: missing %s in: %s",
						tt.filter, id, reply)
				}
			}
			for _, id := range tt.wantNot {
				if strings.Contains(reply, id) {
					t.Errorf("filter=%s: unexpected %s in: %s",
						tt.filter, id, reply)
				}
			}
		})
	}
}

func TestAltFixBridge_ListTasksByStatusEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "t1", "status": "merged", "task_description": "x", "api_cost_usd": 0.0},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.listTasksByStatus(
		context.Background(), "failed",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reply != "No failed tasks." {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAltFixBridge_ListPRs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "t1", "task_description": "fix", "status": "merged", "api_cost_usd": 0.1, "pr_url": "https://github.com/o/r/pull/1"},
				{"id": "t2", "task_description": "wip", "status": "implementing", "api_cost_usd": 0.0, "pr_url": ""},
				{"id": "t3", "task_description": "done", "status": "closed", "api_cost_usd": 0.2, "pr_url": "https://github.com/o/r/pull/3"},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.listPRs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "PRs (2)") {
		t.Errorf("expected 2 PRs: %s", reply)
	}
	if !strings.Contains(reply, "pull/1") {
		t.Errorf("missing PR 1: %s", reply)
	}
	if !strings.Contains(reply, "pull/3") {
		t.Errorf("missing PR 3: %s", reply)
	}
	if strings.Contains(reply, "t2") {
		t.Errorf("should not include task without PR: %s", reply)
	}
}

func TestAltFixBridge_ListPRsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "t1", "status": "failed", "task_description": "x", "api_cost_usd": 0.0, "pr_url": ""},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.listPRs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reply != "No PRs created yet." {
		t.Errorf("unexpected: %s", reply)
	}
}

func TestAltFixBridge_ShareLink(t *testing.T) {
	bridge := &AltFixBridge{
		daemonURL: "http://localhost:9200/api/v1",
	}

	reply, err := bridge.generateShareLink(
		context.Background(), "abc123",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "/ui/tasks/abc123") {
		t.Errorf("missing task URL: %s", reply)
	}
	if !strings.Contains(reply, "requires auth") {
		t.Errorf("missing auth note: %s", reply)
	}
}

func TestAltFixBridge_Checkpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/tasks/abc/checkpoints" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			json.NewEncoder(w).Encode([]map[string]any{
				{"phase": "planning", "status": "done", "timestamp": "10:00", "message": "plan ready"},
				{"phase": "implementing", "status": "running", "timestamp": "10:05", "message": ""},
				{"phase": "testing", "status": "pending", "timestamp": "", "message": ""},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.listCheckpoints(
		context.Background(), "abc",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "[ok] planning") {
		t.Errorf("missing done icon: %s", reply)
	}
	if !strings.Contains(reply, "[..] implementing") {
		t.Errorf("missing running icon: %s", reply)
	}
	if !strings.Contains(reply, "[  ] testing") {
		t.Errorf("missing pending icon: %s", reply)
	}
	if !strings.Contains(reply, "plan ready") {
		t.Errorf("missing message: %s", reply)
	}
}

func TestAltFixBridge_CheckpointsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.listCheckpoints(
		context.Background(), "xyz",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "No checkpoints") {
		t.Errorf("unexpected: %s", reply)
	}
}

func TestAltFixBridge_RetryTask(t *testing.T) {
	var createCalled bool
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "GET" && r.URL.Path == "/tasks/fail99":
				json.NewEncoder(w).Encode(map[string]any{
					"task": map[string]any{
						"task_description": "fix auth",
						"repo_url":        "https://github.com/o/r",
					},
				})
			case r.Method == "POST" && r.URL.Path == "/tasks":
				createCalled = true
				var body map[string]string
				json.NewDecoder(r.Body).Decode(&body)
				if body["task"] != "fix auth" {
					t.Errorf("retry should reuse desc: %s",
						body["task"])
				}
				w.WriteHeader(201)
				json.NewEncoder(w).Encode(map[string]string{
					"id":     "new-task-1",
					"status": "pending",
				})
			default:
				http.Error(w, "not found", 404)
			}
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		repoURL:   "https://github.com/o/r",
		client:    srv.Client(),
	}

	reply, err := bridge.retryTask(context.Background(), "fail99")
	if err != nil {
		t.Fatal(err)
	}
	if !createCalled {
		t.Error("retry should call createTask")
	}
	if !strings.Contains(reply, "Retrying task fail99") {
		t.Errorf("reply should reference original: %s", reply)
	}
	if !strings.Contains(reply, "new-task-1") {
		t.Errorf("reply should contain new task ID: %s", reply)
	}
}

func TestAltFixBridge_RetryEmptyDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"task_description": "",
				},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.retryTask(context.Background(), "empty1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Cannot retry") {
		t.Errorf("should refuse empty desc retry: %s", reply)
	}
}

func TestAltFixBridge_Health(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"status":  "ok",
				"version": "0.6.0",
				"uptime":  "2h30m",
				"workers": 4,
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.checkHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ok", "0.6.0", "2h30m", "4"} {
		if !strings.Contains(reply, want) {
			t.Errorf("missing %q: %s", want, reply)
		}
	}
}

func TestAltFixBridge_HealthUnparseable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("OK"))
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.checkHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "up") {
		t.Errorf("should report daemon up: %s", reply)
	}
}

func TestAltFixBridge_Dashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": "t1", "status": "implementing", "task_description": "a", "api_cost_usd": 0.10, "pr_url": ""},
				{"id": "t2", "status": "merged", "task_description": "b", "api_cost_usd": 0.20, "pr_url": "https://pr/1"},
				{"id": "t3", "status": "failed", "task_description": "c", "api_cost_usd": 0.05, "pr_url": ""},
				{"id": "t4", "status": "completed", "task_description": "d", "api_cost_usd": 0.15, "pr_url": "https://pr/2"},
				{"id": "t5", "status": "queued", "task_description": "e", "api_cost_usd": 0.00, "pr_url": ""},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.dashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"Active:       1":  "active count",
		"Queued:       1":  "queued count",
		"Succeeded:    2":  "succeeded count",
		"Failed:       1":  "failed count",
		"Success rate: 67": "success rate",
		"PRs created:  2":  "PR count",
		"$0.5000":          "total cost",
		"Total tasks:  5":  "total tasks",
	}
	for want, label := range checks {
		if !strings.Contains(reply, want) {
			t.Errorf("missing %s (%q): %s", label, want, reply)
		}
	}
}

func TestAltFixBridge_DashboardEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.dashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Total tasks:  0") {
		t.Errorf("should show 0 tasks: %s", reply)
	}
}

func TestParseFixOpts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantDesc string
		wantOpts map[string]string
	}{
		{
			name:     "no opts",
			input:    "fix the auth bug",
			wantDesc: "fix the auth bug",
			wantOpts: map[string]string{},
		},
		{
			name:     "all opts",
			input:    "fix auth --repo owner/name --model altllm --cost 2.00",
			wantDesc: "fix auth",
			wantOpts: map[string]string{
				"repo": "owner/name", "model": "altllm", "cost": "2.00",
			},
		},
		{
			name:     "opts in middle",
			input:    "fix --model gpt auth bug",
			wantDesc: "fix auth bug",
			wantOpts: map[string]string{"model": "gpt"},
		},
		{
			name:     "dangling flag",
			input:    "fix bug --repo",
			wantDesc: "fix bug",
			wantOpts: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, opts := parseFixOpts(tt.input)
			if desc != tt.wantDesc {
				t.Errorf("desc = %q, want %q", desc, tt.wantDesc)
			}
			for k, v := range tt.wantOpts {
				if opts[k] != v {
					t.Errorf("opt[%s] = %q, want %q", k, opts[k], v)
				}
			}
			if len(opts) != len(tt.wantOpts) {
				t.Errorf("opts len = %d, want %d",
					len(opts), len(tt.wantOpts))
			}
		})
	}
}

func TestAltFixBridge_CreateTaskWithOpts(t *testing.T) {
	var received map[string]string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			received = make(map[string]string)
			json.NewDecoder(r.Body).Decode(&received)
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]string{
				"id":     "task-opt",
				"status": "pending",
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		repoURL:   "https://github.com/default/repo",
		client:    srv.Client(),
	}

	reply, err := bridge.createTaskWithOpts(
		context.Background(),
		"fix auth --repo other/repo --model altllm --cost 5.00",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "task-opt") {
		t.Errorf("reply missing ID: %s", reply)
	}
	if received["repo_url"] != "other/repo" {
		t.Errorf("repo not overridden: %s", received["repo_url"])
	}
	if received["model"] != "altllm" {
		t.Errorf("model not set: %s", received["model"])
	}
	if received["max_cost"] != "5.00" {
		t.Errorf("max_cost not set: %s", received["max_cost"])
	}
}

func TestAltFixBridge_CreateTaskWithOptsEmpty(t *testing.T) {
	bridge := &AltFixBridge{daemonURL: "http://unused"}

	reply, err := bridge.createTaskWithOpts(
		context.Background(), "--repo foo",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "Usage:") {
		t.Errorf("empty desc should show usage: %s", reply)
	}
}

func TestAltFixBridge_MatchFilter(t *testing.T) {
	tests := []struct {
		status string
		filter string
		want   bool
	}{
		{"pending", "active", true},
		{"planning", "active", true},
		{"implementing", "active", true},
		{"reviewing", "active", true},
		{"testing", "active", true},
		{"merged", "active", false},
		{"failed", "active", false},
		{"failed", "failed", true},
		{"merged", "failed", false},
		{"merged", "completed", true},
		{"completed", "completed", true},
		{"closed", "completed", true},
		{"pending", "completed", false},
		{"anything", "unknown", false},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s/%s", tt.status, tt.filter)
		t.Run(name, func(t *testing.T) {
			got := matchFilter(tt.status, tt.filter)
			if got != tt.want {
				t.Errorf("matchFilter(%q, %q) = %v, want %v",
					tt.status, tt.filter, got, tt.want)
			}
		})
	}
}

func TestAltFixBridge_PhaseIcon(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"done", "[ok]"},
		{"completed", "[ok]"},
		{"passed", "[ok]"},
		{"running", "[..]"},
		{"in_progress", "[..]"},
		{"failed", "[!!]"},
		{"pending", "[  ]"},
		{"unknown", "[  ]"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := phaseIcon(tt.status)
			if got != tt.want {
				t.Errorf("phaseIcon(%q) = %q, want %q",
					tt.status, got, tt.want)
			}
		})
	}
}

func TestAltFixBridge_HelpCommandAll16(t *testing.T) {
	result := helpText()
	commands := []string{
		"/fix", "/task", "/status", "/active", "/failed",
		"/completed", "/prs", "/stop", "/steer", "/share",
		"/checkpoints", "/retry", "/health", "/dashboard",
		"/cost", "/help",
	}
	for _, cmd := range commands {
		if !strings.Contains(result, cmd) {
			t.Errorf("help missing %s", cmd)
		}
	}
}

func TestAltFixBridge_HandleMessageRouting(t *testing.T) {
	var lastPath string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			lastPath = r.Method + " " + r.URL.Path
			switch {
			case strings.HasSuffix(r.URL.Path, "/checkpoints"):
				json.NewEncoder(w).Encode([]map[string]any{})
			case r.URL.Path == "/tasks" && r.Method == "GET":
				json.NewEncoder(w).Encode([]map[string]any{})
			case r.URL.Path == "/health":
				json.NewEncoder(w).Encode(map[string]string{
					"status": "ok",
				})
			default:
				json.NewEncoder(w).Encode(map[string]any{
					"task": map[string]any{
						"id": "x", "task_description": "y",
						"status": "ok",
					},
				})
			}
		},
	))
	defer srv.Close()

	var sent []string
	mgr := &Manager{
		channels: make(map[string]Channel),
		workers:  make(map[string]*worker),
		logger:   slog.Default(),
	}
	fakeCh := &fakeChannel{name: "test", sent: &sent}
	mgr.channels["test"] = fakeCh
	mgr.workers["test"] = newWorker("test", fakeCh)

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		repoURL:   "https://github.com/test/repo",
		client:    srv.Client(),
		manager:   mgr,
	}

	routes := []struct {
		text     string
		wantPath string
	}{
		{"/task abc", "GET /tasks/abc"},
		{"/active", "GET /tasks"},
		{"/failed", "GET /tasks"},
		{"/completed", "GET /tasks"},
		{"/prs", "GET /tasks"},
		{"/checkpoints abc", "GET /tasks/abc/checkpoints"},
		{"/health", "GET /health"},
		{"/dashboard", "GET /tasks"},
	}

	for _, rt := range routes {
		msg := InboundMessage{
			ChannelName: "test",
			ChatID:      "c1",
			SenderID:    "s1",
			Text:        rt.text,
		}
		bridge.HandleMessage(context.Background(), msg)
		if lastPath != rt.wantPath {
			t.Errorf("cmd=%q: path=%q, want %q",
				rt.text, lastPath, rt.wantPath)
		}
	}
}
