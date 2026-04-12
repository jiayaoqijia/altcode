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
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": "dev",
	})
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
	if req.RepoURL == "" || req.Task == "" {
		http.Error(w, `{"error":"repo_url and task required"}`, 400)
		return
	}

	task := &Task{
		RepoURL:         req.RepoURL,
		TaskDescription: req.Task,
		Status:          "pending",
		BranchName:      req.Branch,
		AgentConfig:     req.Agents,
		DeliveryID:      req.DeliveryID,
		IssueNumber:     req.IssueNumber,
		RepoOwner:       req.RepoOwner,
		RepoName:        req.RepoName,
	}
	if err := s.store.CreateTask(task); err != nil {
		s.logger.Error("create task", "err", err)
		http.Error(w, `{"error":"failed to create task"}`, 500)
		return
	}

	s.logger.Info("task created", "id", task.ID, "repo", req.RepoURL)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]any{
		"id":     task.ID,
		"status": "pending",
	})
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func (s *Server) handleStopTask(w http.ResponseWriter, r *http.Request) {
	// Stub -- wired in Task 6 (lifecycle).
	id := r.PathValue("id")
	s.logger.Info("stop requested", "id", id)
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
	s.store.AppendEvent(id, "user_steer", string(data))

	s.logger.Info("steer", "task", id, "message", req.Message)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "acknowledged",
	})
}
