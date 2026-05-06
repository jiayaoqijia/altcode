package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	s, err := NewServer(ServerConfig{
		Port:      0,
		DataDir:   t.TempDir(),
		AuthToken: "test",
		// Fast intervals keep SSE-driven handler tests responsive.
		// Iter-2 of karpathy autoresearch: skip the production 5s/2s
		// waits that previously dominated daemon test time.
		PollInterval:    25 * time.Millisecond,
		SSEPollInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// Cleanup mirrors Server.Run shutdown order: cancel runners, wait
	// for dispatch goroutines (POST /tasks fires `go dispatchTask`) to
	// drain, then close the DB. Without this, leaked goroutines write
	// to a closed store between tests and produce noisy "sql: database
	// is closed" warnings that can mask real bugs.
	t.Cleanup(func() {
		s.lifecycleCancel()
		s.dispatchWG.Wait()
		s.store.Close()
	})
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
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
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

// TestHandler_StopTask_QueuedTaskCancelled verifies that stopping a
// task with no live runner marks it cancelled in the store so the
// pending-task poller won't pick it up later. Regression for the
// adversarial review finding that a queued stop was a no-op and that
// a nil-placeholder in s.runners would panic on dereference.
func TestHandler_StopTask_QueuedTaskCancelled(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "queued task",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate the nil-placeholder window inside dispatchTask. Without
	// the handler fix the nil type-assert would panic.
	s.runners.Store(task.ID, (*TaskRunner)(nil))
	defer s.runners.Delete(task.ID)

	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/stop", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("stop: got %d, body: %s", rec.Code, rec.Body.String())
	}
	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
}

// TestStore_MarkCancelled_RaceWithCompletion verifies the SQL-level
// guard that prevents a stop call from silently overwriting a task
// that just transitioned to a terminal status (merged/failed). This
// is the TOCTOU race Codex caught in round-3 review — handleStopTask
// reads task as non-terminal, runner finishes, then MarkCancelled
// would have overwritten the result before the WHERE-clause guard.
func TestStore_MarkCancelled_RaceWithCompletion(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "race",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Simulate the runner finishing successfully just before stop.
	if err := s.store.MarkCompleted(task.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	// MarkCancelled must NOT overwrite the "merged" status.
	if err := s.store.MarkCancelled(task.ID); err != nil {
		t.Fatalf("mark cancelled: %v", err)
	}

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "merged" {
		t.Errorf("status = %q, want merged (MarkCancelled clobbered terminal)",
			got.Status)
	}
}

// TestHandler_StopTask_OrphanedMidPhase verifies that a task stuck in
// a mid-phase status ('planning', 'implementing', etc.) with no live
// runner — the runner-crash scenario — can still be cancelled by the
// user. Round-6 review caught that the earlier CancelPending name only
// matched status='pending', leaving orphans uncancellable.
func TestHandler_StopTask_OrphanedMidPhase(t *testing.T) {
	s := testServer(t)
	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "orphaned mid-phase",
		Status:          "planning", // runner crashed mid-phase
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}

	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/stop", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("stop: got %d, body: %s", rec.Code, rec.Body.String())
	}

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled (orphan should be cancellable)",
			got.Status)
	}
}

// TestStore_CancelIfActive_PreservesFailed verifies that a real
// failure racing a stop request is not clobbered — CancelIfActive
// must refuse to overwrite 'failed' since the handler can't tell
// whether the failure was user-cancel-caused or genuine.
func TestStore_CancelIfActive_PreservesFailed(t *testing.T) {
	s := testServer(t)
	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "failed-preserved",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.store.MarkFailed(task.ID, "real infra error"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	cancelled, err := s.store.CancelIfActive(task.ID)
	if err != nil {
		t.Fatalf("cancel if active: %v", err)
	}
	if cancelled {
		t.Error("CancelIfActive returned true on a failed row — should have refused")
	}
	got, _ := s.store.GetTask(task.ID)
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed (must not be clobbered)", got.Status)
	}
	if got.ErrorMessage != "real infra error" {
		t.Errorf("error_message = %q, want preserved", got.ErrorMessage)
	}
}

// TestHandler_CreateTask_PersistsModelOverride verifies that the
// request's `model` field round-trips through AgentConfig and is
// recoverable by decodeAgentConfig. Round-D review found the field
// was accepted by the API but silently discarded.
func TestHandler_CreateTask_PersistsModelOverride(t *testing.T) {
	s := testServer(t)
	payload := `{"repo_url":"https://github.com/t/r","task":"do it","model":"deepseek-v3","agents":"team"}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, err := s.store.GetTask(resp["id"].(string))
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	mode, model := decodeAgentConfig(got.AgentConfig)
	if mode != "team" {
		t.Errorf("mode = %q, want team", mode)
	}
	if model != "deepseek-v3" {
		t.Errorf("model = %q, want deepseek-v3", model)
	}
}

// TestDecodeAgentConfig_LegacyFormat verifies backward compatibility:
// older tasks stored just the mode string (e.g. "team") without JSON.
func TestDecodeAgentConfig_LegacyFormat(t *testing.T) {
	cases := []struct{ raw, mode, model string }{
		{"", "", ""},
		{"team", "team", ""},
		{"solo", "solo", ""},
		{`{"mode":"pair","model":"claude"}`, "pair", "claude"},
		{`{"model":"gpt-5"}`, "", "gpt-5"},
		{`{"mode":"team"}`, "team", ""},
		{`{"bogus":1`, `{"bogus":1`, ""}, // bad JSON → return raw as mode
	}
	for _, tc := range cases {
		mode, model := decodeAgentConfig(tc.raw)
		if mode != tc.mode || model != tc.model {
			t.Errorf("decode %q → (%q,%q), want (%q,%q)",
				tc.raw, mode, model, tc.mode, tc.model)
		}
	}
}

// TestOrchestrator_MaxCostUSDAbortsRun is the e2e proof iteration 5
// promised: a task with a tiny max_cost_usd and a spawn func that
// returns a big output triggers the cost-budget abort, records a
// budget_exceeded event, and leaves the task `failed`. Closes the
// CC/Codex 9.0 blocker that recordCost existed but wasn't wired.
func TestOrchestrator_MaxCostUSDAbortsRun(t *testing.T) {
	s := testServer(t)

	// 1M-byte output × 1e-6 proxy = $1.00 per phase. Cap at $0.01 so
	// the plan phase's cost sample immediately overruns.
	bigOutput := strings.Repeat("x", 1_000_000)
	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "cost abort test",
		Status:          "pending",
		MaxCostUSD:      0.01,
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}

	orch := NewOrchestrator(s.store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, _ AgentConfig) (string, error) {
			return bigOutput, nil
		},
		Logger:      s.logger,
		MaxFixRetry: 1,
	})
	err := orch.RunTask(context.Background(), task, nil)
	if err == nil || !strings.Contains(err.Error(), "max_cost_usd") {
		t.Fatalf("expected max_cost_usd budget error, got %v", err)
	}

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}

	// Confirm a budget_exceeded event was appended with the cost phase.
	evs, err := s.store.ListEvents(task.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	foundBudget := false
	for _, ev := range evs {
		if ev.EventType == "budget_exceeded" &&
			strings.Contains(ev.Data, "plan") {
			foundBudget = true
		}
	}
	if !foundBudget {
		t.Error("expected a budget_exceeded event tagged with the plan phase")
	}
}

// TestTaskBudget_MaxCostEnforced verifies the autoresearch-iter-4
// fix: recordCost accumulates phase costs and aborts when the
// running total crosses max_cost_usd. Previously the helper had no
// cost path at all and relied only on max_turns.
func TestTaskBudget_MaxCostEnforced(t *testing.T) {
	b := newTaskBudget(0, 1.00) // unlimited turns, $1 cost cap
	if err := b.recordCost(0.40); err != nil {
		t.Fatalf("0.40 should fit in $1 cap: %v", err)
	}
	if err := b.recordCost(0.50); err != nil {
		t.Fatalf("0.90 running total should fit: %v", err)
	}
	if err := b.recordCost(0.20); err == nil {
		t.Error("1.10 running total must trip the $1 cap")
	}
}

// TestTaskBudget_MaxCostNegativeIsClamped ensures a misbehaving
// agent that reports negative cost can't reduce the running total
// or effectively bypass the cap.
func TestTaskBudget_MaxCostNegativeIsClamped(t *testing.T) {
	b := newTaskBudget(0, 1.00)
	_ = b.recordCost(0.80)
	// Negative report must be clamped to 0, so running total stays 0.80.
	if err := b.recordCost(-100.00); err != nil {
		t.Errorf("clamped negative should not trip cap: %v", err)
	}
	// 0.80 + 0.30 = 1.10 > cap.
	if err := b.recordCost(0.30); err == nil {
		t.Error("cap should still fire after clamped negative")
	}
}

// TestTaskBudget_MaxCostZeroIsUnlimited keeps the "0 = unlimited"
// contract — both CC and Codex flagged that silent-unlimited was
// the baseline gotcha we want to keep avoiding.
func TestTaskBudget_MaxCostZeroIsUnlimited(t *testing.T) {
	b := newTaskBudget(0, 0)
	for i := 0; i < 10; i++ {
		if err := b.recordCost(999.99); err != nil {
			t.Fatalf("unlimited cap should never trip: %v", err)
		}
	}
}

// TestBudget_MaxTurnsEnforced verifies the autoresearch-iteration-1
// fix: per-task max_turns is now actually counted and aborts the
// orchestrator when exceeded, with a budget_exceeded event appended.
// Baseline score 7.0 on the Daemon reflected that this field was
// accepted but silently ignored.
func TestBudget_MaxTurnsEnforced(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "budget cap test",
		Status:          "pending",
		MaxTurns:        1, // only allow the plan phase to run
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Use a spawn func that returns a one-step plan so Run enters
	// the implement phase (which should trigger the budget abort).
	orch := NewOrchestrator(s.store, OrchestratorConfig{
		SpawnFunc: func(_ context.Context, cfg AgentConfig) (string, error) {
			if cfg.Role == "lead" {
				return `{"steps":[{"description":"x","prompt":"do x"}]}`, nil
			}
			return "", nil
		},
		Logger:      s.logger,
		MaxFixRetry: 1,
	})
	err := orch.RunTask(context.Background(), task, nil)
	if err == nil || !strings.Contains(err.Error(), "max_turns") {
		t.Fatalf("expected max_turns budget error, got %v", err)
	}

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.ErrorMessage, "max_turns") {
		t.Errorf("error_message = %q, want mention of max_turns", got.ErrorMessage)
	}
}

// TestHandler_CreateTask_SurfacesBudgetEnforcement verifies the
// 201 response now tells callers which limits are actually enforced,
// closing the silent-acceptance gap both CC and Codex flagged.
func TestHandler_CreateTask_SurfacesBudgetEnforcement(t *testing.T) {
	s := testServer(t)
	payload := `{"repo_url":"https://github.com/t/r","task":"x","max_turns":5,"max_cost_usd":1.5}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	enf, ok := resp["budget_enforced"].(map[string]any)
	if !ok {
		t.Fatalf("response missing budget_enforced: %v", resp)
	}
	if enf["max_turns"] != true {
		t.Errorf("max_turns enforced = %v, want true", enf["max_turns"])
	}
	// emitCost feeds recordCost and returns overflow as an error
	// directly, so max_cost_usd is enforced (against a conservative
	// output-size proxy). The 201 response reflects reality.
	if enf["max_cost_usd"] != true {
		t.Errorf("max_cost_usd enforced = %v, want true after iter-5 wiring",
			enf["max_cost_usd"])
	}
	// A "warnings" array is still returned explaining the proxy-cost
	// caveat so callers don't assume ground-truth agent cost.
	if _, ok := resp["warnings"]; !ok {
		t.Error("expected warnings array describing the proxy-cost caveat")
	}
}

// TestStore_MarkCancelled_OverridesFailed verifies the intentional
// `failed → cancelled` reclassification. When the orchestrator writes
// MarkFailed on ctx.Err() and then TaskRunner.Run fires MarkCancelled
// because stopped=true, the user's cancel intent must win. Codex
// round-4 flagged missing coverage of this path.
func TestStore_MarkCancelled_OverridesFailed(t *testing.T) {
	s := testServer(t)
	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "failed-then-cancelled",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.store.MarkFailed(task.ID, "plan failed: context canceled"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := s.store.MarkCancelled(task.ID); err != nil {
		t.Fatalf("mark cancelled: %v", err)
	}
	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled (failed→cancelled override)",
			got.Status)
	}
	if got.ErrorMessage != "" {
		t.Errorf("error_message = %q, want empty (should be cleared)",
			got.ErrorMessage)
	}
}

// TestRunner_StopBeforeRun_StoppedFlagPath verifies the in-memory
// `stopped` atomic short-circuit in TaskRunner.Run — the path where
// Stop is called on the real runner *before* Run starts, distinct
// from the store-side isTerminal path.
func TestRunner_StopBeforeRun_StoppedFlagPath(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "stop-before-run",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}

	tr := NewTaskRunner(task, s.store, s.orch, s.logger)
	tr.Stop() // sets stopped=true before Run installs cancel
	tr.Run(context.Background())

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled (stopped flag path)",
			got.Status)
	}
	if got.StartedAt != nil {
		t.Errorf("started_at = %v, want nil — runner should not have started",
			got.StartedAt)
	}
	if got.ErrorMessage != "" {
		t.Errorf("error_message = %q, want empty (MarkCancelled should clear)",
			got.ErrorMessage)
	}
}

// TestHandler_StopBeforeRun_HonoursCancellation verifies that a Stop
// landing during the nil-placeholder window (after dispatch claimed the
// task ID but before the real TaskRunner is stored) is honoured by the
// runner once it starts: the runner re-reads store status and returns
// without executing.
func TestHandler_StopBeforeRun_HonoursCancellation(t *testing.T) {
	s := testServer(t)

	task := &Task{
		RepoURL:         "https://github.com/t/r",
		TaskDescription: "queued + stop race",
		Status:          "pending",
	}
	if err := s.store.CreateTask(task); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Simulate dispatch having claimed the slot with the nil placeholder.
	s.runners.Store(task.ID, (*TaskRunner)(nil))

	// User Stop arrives in the placeholder window.
	req := httptest.NewRequest("POST", "/tasks/"+task.ID+"/stop", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("stop: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// Now the dispatch goroutine "wakes up" and creates the real runner
	// (mirroring server.go dispatchTask after TryAcquire).
	s.runners.Store(task.ID, NewTaskRunner(task, s.store, s.orch, s.logger))
	defer s.runners.Delete(task.ID)

	runner, _ := s.runners.Load(task.ID)
	tr := runner.(*TaskRunner)

	// Run should observe the store-side cancellation and bail without
	// transitioning to a non-terminal status.
	tr.Run(context.Background())

	got, err := s.store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled (Stop-before-Run race)",
			got.Status)
	}
	if got.StartedAt != nil {
		t.Errorf("started_at = %v, want nil — runner should not have started",
			got.StartedAt)
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
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
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

func TestHandler_CreateTask_IncludesQueuePosition(t *testing.T) {
	s := testServer(t)

	payload := `{"repo_url":"https://github.com/t/r","task":"fix bug"}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("create: got %d, body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// queue_position must be present in the response.
	qp, ok := body["queue_position"]
	if !ok {
		t.Fatal("expected queue_position in create response")
	}
	pos := int(qp.(float64))
	if pos < 0 {
		t.Errorf("queue_position = %d, want >= 0", pos)
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
