package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// Surface encode errors (e.g. broken connection) in the daemon
	// log so a silent health-probe drop doesn't look like success.
	// CC iter-9 flagged the unchecked Encode as the last daemon gap.
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": "dev",
	}); err != nil {
		s.logger.Warn("health write", "err", err)
	}
}

type createTaskRequest struct {
	RepoURL     string  `json:"repo_url"`
	Task        string  `json:"task"`
	Branch      string  `json:"branch"`
	Agents      string  `json:"agents"`
	Model       string  `json:"model"`
	MaxCostUSD  float64 `json:"max_cost_usd"`
	MaxTurns    int     `json:"max_turns"`
	DeliveryID  string  `json:"delivery_id"`
	IssueNumber int     `json:"issue_number"`
	RepoOwner   string  `json:"repo_owner"`
	RepoName    string  `json:"repo_name"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, 400)
		return
	}
	req.RepoURL = strings.TrimSpace(req.RepoURL)
	req.Task = strings.TrimSpace(req.Task)
	if req.RepoURL == "" || req.Task == "" {
		http.Error(w, `{"error":"repo_url and task required"}`, 400)
		return
	}
	// Reject nonsensical budget values. Previously negatives were
	// silently treated as 0 (unlimited) which is a contract bug —
	// a caller passing `max_turns: -1` would expect rejection, not
	// unlimited budget. Codex iteration-1 review flagged this.
	if req.MaxTurns < 0 {
		http.Error(w, `{"error":"max_turns must be >= 0"}`, 400)
		return
	}
	if req.MaxCostUSD < 0 {
		http.Error(w, `{"error":"max_cost_usd must be >= 0"}`, 400)
		return
	}

	// Encode per-task overrides into AgentConfig as JSON so the
	// orchestrator can pick them up without a schema migration.
	// agents=<mode> stays as the top-level "mode" key for backward
	// compatibility; model is the optional per-task override.
	agentJSON := encodeAgentConfig(req.Agents, req.Model)

	task := &Task{
		RepoURL:         req.RepoURL,
		TaskDescription: req.Task,
		Status:          "pending",
		BranchName:      req.Branch,
		AgentConfig:     agentJSON,
		DeliveryID:      req.DeliveryID,
		IssueNumber:     req.IssueNumber,
		RepoOwner:       req.RepoOwner,
		RepoName:        req.RepoName,
		MaxCostUSD:      req.MaxCostUSD, // 0 = unlimited
		MaxTurns:        req.MaxTurns,   // 0 = unlimited; enforced by orchestrator
	}
	if err := s.store.CreateTask(task); err != nil {
		// Duplicate delivery_id hits the UNIQUE constraint — return 409
		// instead of 500 so clients can distinguish dedup from real errors.
		// Found by Codex E2E test #14 and confirmed by daemon orchestration.
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			http.Error(w, `{"error":"duplicate delivery_id"}`, 409)
			return
		}
		s.logger.Error("create task", "err", err)
		http.Error(w, `{"error":"failed to create task"}`, 500)
		return
	}

	s.logger.Info("task created", "id", task.ID, "repo", req.RepoURL)

	// Count pending-and-older tasks before the async dispatch races
	// this task into an active state. cm.QueuePosition was broken —
	// it computed len(active)-maxTasks which is always <=0 because
	// the semaphore caps active at maxTasks. Use the store instead.
	queuePos, err := s.store.CountPendingBefore(task.ID)
	if err != nil {
		s.logger.Warn("queue position", "task", task.ID, "err", err)
		queuePos = 0
	}

	// Dispatch task execution in the background. Add to the WaitGroup
	// here (before `go`) so shutdown's Wait can't race past us.
	s.dispatchWG.Add(1)
	go s.dispatchTask(task)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	resp := map[string]any{
		"id":             task.ID,
		"status":         "pending",
		"queue_position": queuePos,
	}
	// Explicitly tell the caller which optional limits are enforced.
	// max_turns → enforced (orchestrator counts phase/step turns).
	// max_cost_usd → enforced: emitCost feeds recordCost at plan,
	// each implement step, and review; the aggregated overflow
	// returns a budget_exceeded error from emitCost itself, which
	// every call site routes through failBudget+return. Estimate is
	// a conservative (output_bytes × 1e-6) USD proxy since altcode
	// subprocesses don't report ground-truth cost back yet; the
	// surface is wired so a future agent-cost hook drops in without
	// another migration.
	resp["budget_enforced"] = map[string]bool{
		"max_turns":    task.MaxTurns > 0,
		"max_cost_usd": task.MaxCostUSD > 0,
	}
	if task.MaxCostUSD > 0 {
		resp["warnings"] = []string{
			"max_cost_usd enforced against a conservative output-size proxy — " +
				"real agent cost reporting is deferred, so the cap trips only on large output overruns",
		}
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleListTasks(w http.ResponseWriter, _ *http.Request) {
	tasks, err := s.store.ListTasks()
	if err != nil {
		http.Error(w, `{"error":"failed to list tasks"}`, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// QueueInfo describes a task's position in the pending queue.
type QueueInfo struct {
	Position    int `json:"queue_position"`
	EstWaitSecs int `json:"est_wait_seconds"`
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.store.GetTask(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"task not found"}`, 404)
		} else {
			http.Error(w, `{"error":"internal error"}`, 500)
		}
		return
	}

	// Build response with optional queue info.
	resp := map[string]any{
		"task": task,
	}
	if task.Status == "pending" {
		pos, err := s.store.CountPendingBefore(task.ID)
		if err == nil {
			resp["queue"] = QueueInfo{
				Position:    pos + 1, // 1-indexed
				EstWaitSecs: (pos + 1) * 300,
			}
		}
	} else {
		resp["queue"] = QueueInfo{Position: 0, EstWaitSecs: 0}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleStopTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	task, err := s.store.GetTask(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"task not found"}`, 404)
		} else {
			http.Error(w, `{"error":"internal error"}`, 500)
		}
		return
	}
	if isTerminal(task.Status) {
		http.Error(w, `{"error":"task already completed"}`, 409)
		return
	}

	// Cancel the running task if a runner exists. `dispatchTask` stores
	// a `(*TaskRunner)(nil)` placeholder while it decides whether to
	// acquire a concurrency slot, so we must guard against nil before
	// dereferencing. If the task is queued (no live runner), mark it
	// cancelled in the store so pollPendingTasks won't pick it up.
	stopped := false
	if v, ok := s.runners.Load(id); ok {
		if runner, ok := v.(*TaskRunner); ok && runner != nil {
			runner.Stop()
			stopped = true
		}
	}
	if !stopped {
		// CancelIfActive transitions anything still in-flight (queued
		// OR mid-phase — e.g. a runner that crashed leaving the task
		// stuck in 'planning'). It refuses to overwrite terminal
		// rows including 'failed', so a genuine failure that raced
		// this stop request isn't silently rewritten as user cancel.
		cancelled, err := s.store.CancelIfActive(id)
		if err != nil {
			s.logger.Warn("cancel queued task",
				"task", id, "err", err)
		} else if cancelled {
			if err := s.store.AppendEvent(
				id, "cancelled_by_user", "",
			); err != nil {
				s.logger.Warn("append cancel event",
					"task", id, "err", err)
			}
		}
	}

	s.logger.Info("stop requested", "id", id, "was_running", stopped)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	json.NewEncoder(w).Encode(map[string]string{"status": "stopping"})
}

func (s *Server) handleSteerTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		strings.TrimSpace(req.Message) == "" {
		http.Error(w, `{"error":"message required"}`, 400)
		return
	}

	task, err := s.store.GetTask(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"task not found"}`, 404)
		} else {
			http.Error(w, `{"error":"internal error"}`, 500)
		}
		return
	}
	if isTerminal(task.Status) {
		http.Error(w, `{"error":"task already completed"}`, 409)
		return
	}

	data, _ := json.Marshal(map[string]string{
		"message": req.Message,
	})
	if err := s.store.AppendEvent(id, "user_steer", string(data)); err != nil {
		s.logger.Error("append steer event", "task", id, "err", err)
		http.Error(w, `{"error":"failed to record steer"}`, 500)
		return
	}

	// Forward to running orchestrator if task is active.
	if v, ok := s.runners.Load(id); ok {
		if runner, ok := v.(*TaskRunner); ok && runner != nil {
			runner.Steer(req.Message)
		}
	}

	s.logger.Info("steer", "task", id, "message", req.Message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "acknowledged",
	})
}
