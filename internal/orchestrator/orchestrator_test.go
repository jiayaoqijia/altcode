package orchestrator_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
