package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

func (s *Server) handleListCheckpoints(
	w http.ResponseWriter, r *http.Request,
) {
	taskID := r.PathValue("id")
	if _, err := s.store.GetTask(taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"task not found"}`, 404)
		} else {
			http.Error(w, `{"error":"internal error"}`, 500)
		}
		return
	}
	cps, err := s.store.ListCheckpoints(taskID)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, 500)
		return
	}
	if cps == nil {
		cps = []*Checkpoint{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cps)
}

func (s *Server) handleRestoreCheckpoint(
	w http.ResponseWriter, r *http.Request,
) {
	taskID := r.PathValue("id")
	var req struct {
		CheckpointID string `json:"checkpoint_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.CheckpointID == "" {
		http.Error(w, `{"error":"checkpoint_id required"}`, 400)
		return
	}

	task, err := s.store.GetTask(taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"task not found"}`, 404)
		} else {
			http.Error(w, `{"error":"internal error"}`, 500)
		}
		return
	}

	cp, err := s.store.GetCheckpoint(req.CheckpointID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"checkpoint not found"}`, 404)
		} else {
			http.Error(w, `{"error":"internal error"}`, 500)
		}
		return
	}
	if cp.TaskID != task.ID {
		http.Error(w, `{"error":"checkpoint does not belong to task"}`, 400)
		return
	}

	// Record restore intent as an event. Actual git restore is
	// deferred to Plan D.2.
	data, _ := json.Marshal(map[string]string{
		"checkpoint_id": cp.ID,
		"git_sha":       cp.GitSHA,
		"phase":         cp.Phase,
	})
	s.store.AppendEvent(taskID, "restore_requested", string(data))

	s.logger.Info("restore requested",
		"task", taskID, "checkpoint", cp.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	json.NewEncoder(w).Encode(map[string]string{
		"status":        "restore_queued",
		"checkpoint_id": cp.ID,
	})
}
