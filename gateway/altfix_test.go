package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAltFixBridge_HelpCommand(t *testing.T) {
	result := helpText()
	if !strings.Contains(result, "/fix") {
		t.Fatal("help should mention /fix")
	}
	if !strings.Contains(result, "/status") {
		t.Fatal("help should mention /status")
	}
}

func TestAltFixBridge_CreateTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/tasks" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}

			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)

			if req["task"] != "fix the bug" {
				t.Errorf("unexpected task: %s", req["task"])
			}

			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]string{
				"id":     "task-123",
				"status": "pending",
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		repoURL:   "https://github.com/test/repo",
		client:    srv.Client(),
	}

	reply, err := bridge.createTask(context.Background(), "fix the bug")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "task-123") {
		t.Errorf("reply should contain task ID: %s", reply)
	}
}

func TestAltFixBridge_ListTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":               "task-abc",
					"task_description": "fix stuff",
					"status":           "running",
					"api_cost_usd":     0.05,
				},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.listTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "task-abc") {
		t.Errorf("reply should contain task ID: %s", reply)
	}
	if !strings.Contains(reply, "running") {
		t.Errorf("reply should contain status: %s", reply)
	}
}

func TestAltFixBridge_EmptyTasks(t *testing.T) {
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

	reply, err := bridge.listTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reply != "No active tasks." {
		t.Errorf("unexpected reply: %s", reply)
	}
}

func TestAltFixBridge_ShowCost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{"api_cost_usd": 0.10},
				{"api_cost_usd": 0.25},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.showCost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "$0.3500") {
		t.Errorf("unexpected cost: %s", reply)
	}
}

func TestAltFixBridge_SteerParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/steer") {
				t.Errorf("expected /steer path, got %s", r.URL.Path)
			}
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "acknowledged",
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	reply, err := bridge.steerTask(
		context.Background(), "/steer abc123 focus on tests",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "abc123") {
		t.Errorf("reply should reference task ID: %s", reply)
	}
}

func TestAltFixBridge_DaemonError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"not found"}`, 404)
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	_, err := bridge.stopTask(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status code: %v", err)
	}
}
