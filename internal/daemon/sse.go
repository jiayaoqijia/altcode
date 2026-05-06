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

	// Track this stream in dispatchWG so Run()'s shutdown sequence
	// waits for it to finish before closing the store. Without this
	// an SSE client holding the connection open past http.Server.Shutdown's
	// 10s grace window can land a ListEvents/GetTask call against an
	// already-closed DB. Paired with the lifecycleCtx watch below.
	s.dispatchWG.Add(1)
	defer s.dispatchWG.Done()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // bypass nginx buffering

	// Replay from Last-Event-ID if provided.
	var lastID int64
	if idStr := r.Header.Get("Last-Event-ID"); idStr != "" {
		if parsed, err := strconv.ParseInt(idStr, 10, 64); err != nil {
			s.logger.Warn("invalid Last-Event-ID", "value", idStr, "err", err)
		} else {
			lastID = parsed
		}
	}

	// Use server-configurable interval so tests can poll at 25ms
	// without changing production semantics (default still 2s).
	// Karpathy autoresearch iter-2: SSE tests no longer pay a 2s
	// wait per heartbeat assertion.
	interval := s.cfg.SSEPollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.lifecycleCtx.Done():
			// Daemon is shutting down — exit cooperatively so
			// Run()'s dispatchWG.Wait unblocks before store.Close.
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
			if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
				return // client disconnected
			}
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
