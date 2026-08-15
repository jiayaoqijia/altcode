package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/config"
	"github.com/jiayaoqijia/altcode/internal/orchestrator"
)

func sseText(text string) string {
	return fmt.Sprintf("event: content_block_start\ndata: %s\n\n", `{"index":0,"content_block":{"type":"text","text":""}}`) +
		fmt.Sprintf("event: content_block_delta\ndata: %s\n\n", fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text)) +
		"event: content_block_stop\ndata: {}\n\nevent: message_stop\ndata: {}\n\n"
}

func sseToolCall(toolID, toolName, input string) string {
	return fmt.Sprintf(
		"event: content_block_start\ndata: %s\n\n",
		fmt.Sprintf(`{"index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, toolID, toolName),
	) +
		fmt.Sprintf(
			"event: content_block_delta\ndata: %s\n\n",
			fmt.Sprintf(`{"delta":{"type":"input_json_delta","partial_json":%q}}`, input),
		) +
		"event: content_block_stop\ndata: {}\n\n" +
		"event: message_stop\ndata: {}\n\n"
}

func mockModel(t *testing.T, response string) *config.Config {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseText(response)))
	}))
	t.Cleanup(srv.Close)

	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
	return cfg
}

func TestNewSession(t *testing.T) {
	s := orchestrator.NewSession(nil)
	if s == nil {
		t.Fatal("nil session")
	}
}

func TestRunParallel_MultipleModels(t *testing.T) {
	assignments := []orchestrator.ModelAssignment{
		{Role: orchestrator.RoleArchitect, Model: "model-a", Config: mockModel(t, "The architecture looks good. I approve this design.")},
		{Role: orchestrator.RoleReviewer, Model: "model-b", Config: mockModel(t, "I found a concern: missing error handling in the parser.")},
		{Role: orchestrator.RoleChallenger, Model: "model-c", Config: mockModel(t, "This will fail under load. There's a race condition in the cache.")},
	}

	s := orchestrator.NewSession(assignments)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	findings, err := s.RunParallel(ctx, "Review this code.")
	if err != nil {
		t.Fatal(err)
	}

	if len(findings) != 3 {
		t.Fatalf("Expected 3 findings, got %d", len(findings))
	}

	roles := make(map[orchestrator.Role]bool)
	for _, f := range findings {
		roles[f.Role] = true
		if f.Content == "" {
			t.Errorf("Empty content for %s", f.Model)
		}
	}

	if !roles[orchestrator.RoleArchitect] || !roles[orchestrator.RoleReviewer] || !roles[orchestrator.RoleChallenger] {
		t.Error("Missing roles in findings")
	}
}

func TestRunParallel_ScopesToolsToProjectRoot(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "README.md"), []byte("home marker\n"), 0o644); err != nil {
		t.Fatalf("write home readme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("root marker\n"), 0o644); err != nil {
		t.Fatalf("write root readme: %v", err)
	}
	t.Chdir(home)

	var mu sync.Mutex
	var requests [][]byte
	responses := []string{
		sseToolCall("read-1", "read", `{"file_path":"README.md"}`),
		sseText("done"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, body)
		idx := len(requests) - 1
		mu.Unlock()

		if idx >= len(responses) {
			t.Errorf("unexpected model request %d", idx)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responses[idx]))
	}))
	defer srv.Close()

	parent := config.Default()
	parent.Provider["anthropic"] = config.ProviderConfig{APIKey: "k", BaseURL: srv.URL}
	team := &config.TeamConfig{
		Models: map[string]config.TeamModel{
			"reviewer": {Model: "anthropic/test"},
		},
	}
	session := orchestrator.NewSessionFromConfig(team, parent, root)

	if _, err := session.RunParallel(context.Background(), "read readme"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 2 {
		t.Fatalf("expected second request with tool result, got %d requests", len(requests))
	}
	var second map[string]any
	if err := json.Unmarshal(requests[1], &second); err != nil {
		t.Fatalf("unmarshal second request: %v", err)
	}
	encoded := fmt.Sprint(second["messages"])
	if !strings.Contains(encoded, "root marker") {
		t.Fatalf("tool result did not include project-root file; request=%s", string(requests[1]))
	}
	if strings.Contains(encoded, "home marker") {
		t.Fatalf("tool result leaked cwd/home file; request=%s", string(requests[1]))
	}
}

func TestRunParallel_ClassifiesResponses(t *testing.T) {
	assignments := []orchestrator.ModelAssignment{
		{Role: orchestrator.RoleEvaluator, Model: "approver", Config: mockModel(t, "LGTM. I approve this change.")},
		{Role: orchestrator.RoleChallenger, Model: "rejector", Config: mockModel(t, "I reject this. There's a critical bug.")},
		{Role: orchestrator.RoleReviewer, Model: "concerned", Config: mockModel(t, "I have a concern about the error handling.")},
	}

	s := orchestrator.NewSession(assignments)
	findings, _ := s.RunParallel(context.Background(), "Evaluate.")

	types := make(map[string]bool)
	for _, f := range findings {
		types[f.Type] = true
	}

	if !types["approval"] {
		t.Error("Should classify 'approve' as approval")
	}
	if !types["rejection"] {
		t.Error("Should classify 'reject' as rejection")
	}
	if !types["concern"] {
		t.Error("Should classify 'concern' as concern")
	}
}

func TestSynthesize_Approve(t *testing.T) {
	assignments := []orchestrator.ModelAssignment{
		{Role: orchestrator.RoleArchitect, Model: "a", Config: mockModel(t, "Looks great. I approve.")},
		{Role: orchestrator.RoleReviewer, Model: "b", Config: mockModel(t, "LGTM, approved.")},
		{Role: orchestrator.RoleEvaluator, Model: "c", Config: mockModel(t, "PASS. All checks pass.")},
	}

	s := orchestrator.NewSession(assignments)
	s.RunParallel(context.Background(), "Review.")

	verdict := s.Synthesize()
	if verdict.Decision != "approve" {
		t.Errorf("Expected approve, got %q", verdict.Decision)
	}
	if verdict.Agreement < 0.8 {
		t.Errorf("Agreement too low: %.2f", verdict.Agreement)
	}
}

func TestSynthesize_Iterate(t *testing.T) {
	assignments := []orchestrator.ModelAssignment{
		{Role: orchestrator.RoleArchitect, Model: "a", Config: mockModel(t, "I approve the design.")},
		{Role: orchestrator.RoleChallenger, Model: "b", Config: mockModel(t, "There's a bug in line 42.")},
	}

	s := orchestrator.NewSession(assignments)
	s.RunParallel(context.Background(), "Review.")

	verdict := s.Synthesize()
	if verdict.Decision != "iterate" {
		t.Errorf("Expected iterate, got %q", verdict.Decision)
	}
}

func TestSynthesize_Reject(t *testing.T) {
	assignments := []orchestrator.ModelAssignment{
		{Role: orchestrator.RoleChallenger, Model: "a", Config: mockModel(t, "I reject this. Security issue.")},
		{Role: orchestrator.RoleReviewer, Model: "b", Config: mockModel(t, "Critical bug found. Fail.")},
		{Role: orchestrator.RoleEvaluator, Model: "c", Config: mockModel(t, "FAIL. Multiple problems.")},
	}

	s := orchestrator.NewSession(assignments)
	s.RunParallel(context.Background(), "Review.")

	verdict := s.Synthesize()
	if verdict.Decision != "reject" {
		t.Errorf("Expected reject, got %q", verdict.Decision)
	}
}

func TestCrossCheck(t *testing.T) {
	assignments := []orchestrator.ModelAssignment{
		{Role: orchestrator.RoleArchitect, Model: "a", Config: mockModel(t, "The design is sound.")},
		{Role: orchestrator.RoleChallenger, Model: "b", Config: mockModel(t, "I disagree. There's a concern about scalability.")},
	}

	s := orchestrator.NewSession(assignments)
	s.RunParallel(context.Background(), "Initial review.")

	crossFindings, err := s.CrossCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Cross-check should produce additional findings
	if len(crossFindings) < 2 {
		t.Errorf("Expected 2+ cross-check findings, got %d", len(crossFindings))
	}

	// Total findings should be initial + cross-check
	all := s.Findings()
	if len(all) < 4 {
		t.Errorf("Expected 4+ total findings, got %d", len(all))
	}
}

func TestConcurrentSessions(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := orchestrator.NewSession([]orchestrator.ModelAssignment{
				{Role: orchestrator.RoleReviewer, Model: "m", Config: mockModel(t, "ok")},
			})
			s.RunParallel(context.Background(), "test")
			s.Synthesize()
		}()
	}
	wg.Wait()
}

func TestVerdictSummary(t *testing.T) {
	assignments := []orchestrator.ModelAssignment{
		{Role: orchestrator.RoleArchitect, Model: "claude", Config: mockModel(t, "Approved.")},
		{Role: orchestrator.RoleChallenger, Model: "deepseek", Config: mockModel(t, "Concern: missing tests.")},
	}

	s := orchestrator.NewSession(assignments)
	s.RunParallel(context.Background(), "Review.")
	verdict := s.Synthesize()

	if verdict.Summary == "" {
		t.Error("Empty summary")
	}
	if verdict.Timestamp.IsZero() {
		t.Error("Zero timestamp")
	}
}
