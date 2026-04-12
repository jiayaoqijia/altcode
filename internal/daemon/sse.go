package daemon

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// handleSSE streams task events as Server-Sent Events.
// Supports Last-Event-ID for replay after reconnection.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")

	// Verify task exists.
	if _, err := s.store.GetTask(taskID); err != nil {
		http.Error(w, `{"error":"task not found"}`, 404)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Replay from Last-Event-ID if provided.
	var lastID int64
	if idStr := r.Header.Get("Last-Event-ID"); idStr != "" {
		lastID, _ = strconv.ParseInt(idStr, 10, 64)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			events, err := s.store.ListEvents(taskID, lastID)
			if err != nil {
				return
			}
			for _, ev := range events {
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n",
					ev.ID, ev.EventType, ev.Data)
				lastID = ev.ID
			}
			if len(events) > 0 {
				flusher.Flush()
			}

			// Heartbeat to detect dead connections.
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

			// Stop streaming when task reaches a terminal status.
			task, err := s.store.GetTask(taskID)
			if err != nil {
				return
			}
			if isTerminal(task.Status) {
				// Drain any remaining events before closing.
				final, err := s.store.ListEvents(taskID, lastID)
				if err != nil {
					return
				}
				for _, ev := range final {
					fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n",
						ev.ID, ev.EventType, ev.Data)
				}
				if len(final) > 0 {
					flusher.Flush()
				}
				return
			}
		}
	}
}

// isTerminal reports whether a task status is final.
func isTerminal(status string) bool {
	switch status {
	case "merged", "closed", "failed", "cancelled":
		return true
	}
	return false
}
