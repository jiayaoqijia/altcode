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

// --- Blocker fix tests ---

func TestSafeTrunc(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"abcdefghij", 8, "abcdefgh"},
		{"abc", 8, "abc"},
		{"12345678", 8, "12345678"},
		{"", 8, ""},
		{"ab", 2, "ab"},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		got := safeTrunc(tt.input, tt.n)
		if got != tt.want {
			t.Errorf("safeTrunc(%q, %d) = %q, want %q",
				tt.input, tt.n, got, tt.want)
		}
	}
}

func TestAltFixBridge_ShortTaskID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":               "ab",
					"task_description": "short id task",
					"status":           "running",
					"api_cost_usd":     0.01,
				},
			})
		},
	))
	defer srv.Close()

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		client:    srv.Client(),
	}

	// Must not panic on ID shorter than 8 chars.
	reply, err := bridge.listTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "ab") {
		t.Errorf("reply should contain short ID 'ab': %s", reply)
	}
}

func TestAltFixBridge_RateLimitRejectsFlood(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{
				"id":     "task-999",
				"status": "pending",
			})
		},
	))
	defer srv.Close()

	var sent []string
	mgr := &Manager{
		channels: make(map[string]Channel),
		workers:  make(map[string]*worker),
		logger:   slog.Default(),
	}
	// Use a fake channel that records sent messages.
	fakeCh := &fakeChannel{name: "test", sent: &sent}
	mgr.channels["test"] = fakeCh
	mgr.workers["test"] = newWorker("test", fakeCh)

	bridge := &AltFixBridge{
		daemonURL: srv.URL,
		repoURL:   "https://github.com/test/repo",
		client:    srv.Client(),
		manager:   mgr,
		rateLimiter: NewRateLimiter(RateLimitConfig{
			MaxAttempts:    2,
			WindowSeconds:  60,
			LockoutSeconds: 60,
		}),
	}

	msg := InboundMessage{
		ChannelName: "test",
		ChatID:      "chat1",
		SenderID:    "user-flood",
		Text:        "/help",
	}

	// First 2 should pass (under limit).
	bridge.HandleMessage(context.Background(), msg)
	bridge.HandleMessage(context.Background(), msg)
	// 3rd should be rate limited.
	bridge.HandleMessage(context.Background(), msg)

	if len(sent) < 3 {
		t.Fatalf("expected at least 3 replies, got %d", len(sent))
	}
	// The 3rd reply must be the rate limit message.
	if !strings.Contains(sent[2], "Rate limited") {
		t.Errorf("3rd reply should be rate limit, got: %s", sent[2])
	}
}

func TestAltFixBridge_SanitizedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "daemon status stripped",
			err:  fmt.Errorf("daemon returned 500: {\"stack\":\"...\"}"),
			want: "daemon returned 500",
		},
		{
			name: "generic error sanitized",
			err:  fmt.Errorf("connection refused at 10.0.0.5:9200"),
			want: "internal error",
		},
		{
			name: "daemon 404",
			err:  fmt.Errorf("daemon returned 404: not found"),
			want: "daemon returned 404",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeError(tt.err)
			if got != tt.want {
				t.Errorf("sanitizeError(%v) = %q, want %q",
					tt.err, got, tt.want)
			}
		})
	}
}

// fakeChannel implements Channel for testing Manager.Send calls.
type fakeChannel struct {
	name string
	sent *[]string
}

func (f *fakeChannel) Name() string                                     { return f.name }
func (f *fakeChannel) Start(_ context.Context) error                    { return nil }
func (f *fakeChannel) Stop(_ context.Context) error                     { return nil }
func (f *fakeChannel) IsRunning() bool                                  { return true }
func (f *fakeChannel) Send(_ context.Context, msg OutboundMessage) error {
	*f.sent = append(*f.sent, msg.Text)
	return nil
}
