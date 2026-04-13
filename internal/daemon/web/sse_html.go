package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"
)

// EventStore abstracts event and task lookups for the SSE
// endpoint. Task 8 wires the real daemon.Store implementation.
type EventStore interface {
	GetTask(id string) (*TaskView, error)
	ListEvents(taskID string, afterID int64) ([]*EventView, error)
}

// EventView is the read-model for a single task event.
type EventView struct {
	ID        int64
	TaskID    string
	EventType string
	Data      string
	CreatedAt time.Time
}

// ssePartialTmpl is a standalone template for rendering one event
// item as an HTML fragment. It is parsed once at init time.
var ssePartialTmpl *template.Template

func init() {
	const src = `{{if eq .EventType "phase_started" "phase_completed"}}` +
		`<div class="feed-item feed-item-phase" data-event-id="{{.ID}}">` +
		`<strong>{{.EventType}}</strong>: {{.Data}} ` +
		`<span class="text-xs text-gray-400" style="float:right">{{.CreatedAt.Format "15:04:05"}}</span>` +
		`</div>` +
		`{{else if eq .EventType "agent_output" "tool_call"}}` +
		`<div class="feed-item feed-item-agent" data-event-id="{{.ID}}">` +
		`<pre style="margin:0;white-space:pre-wrap;font-family:monospace;font-size:0.8rem">{{.Data}}</pre>` +
		`<span class="text-xs text-gray-400" style="float:right">{{.CreatedAt.Format "15:04:05"}}</span>` +
		`</div>` +
		`{{else if eq .EventType "error"}}` +
		`<div class="feed-item feed-item-error" data-event-id="{{.ID}}">` +
		`<span style="color:#ef4444;font-weight:600">Error:</span> {{.Data}} ` +
		`<span class="text-xs text-gray-400" style="float:right">{{.CreatedAt.Format "15:04:05"}}</span>` +
		`</div>` +
		`{{else if eq .EventType "user_steer"}}` +
		`<div class="feed-item feed-item-steer" data-event-id="{{.ID}}">` +
		`<div style="background:#dbeafe;color:#1e40af;padding:0.5rem 0.75rem;border-radius:0.375rem;display:inline-block;max-width:80%">` +
		`{{.Data}}</div> ` +
		`<span class="text-xs text-gray-400" style="float:right">{{.CreatedAt.Format "15:04:05"}}</span>` +
		`</div>` +
		`{{else if eq .EventType "pr_created"}}` +
		`<div class="feed-item feed-item-info" data-event-id="{{.ID}}">` +
		`<span style="color:#10b981;font-weight:500">PR created:</span> {{.Data}} ` +
		`<span class="text-xs text-gray-400" style="float:right">{{.CreatedAt.Format "15:04:05"}}</span>` +
		`</div>` +
		`{{else if eq .EventType "ci_status"}}` +
		`<div class="feed-item feed-item-info" data-event-id="{{.ID}}">` +
		`<span style="color:#3b82f6;font-weight:500">CI:</span> {{.Data}} ` +
		`<span class="text-xs text-gray-400" style="float:right">{{.CreatedAt.Format "15:04:05"}}</span>` +
		`</div>` +
		`{{else}}` +
		`<div class="feed-item" data-event-id="{{.ID}}" style="color:#6b7280">` +
		`<span class="text-xs font-medium">[{{.EventType}}]</span> {{.Data}} ` +
		`<span class="text-xs text-gray-400" style="float:right">{{.CreatedAt.Format "15:04:05"}}</span>` +
		`</div>` +
		`{{end}}`
	ssePartialTmpl = template.Must(template.New("sse_event").Parse(src))
}

// renderEventHTML renders a single EventView to an HTML string.
func renderEventHTML(ev *EventView) (string, error) {
	var buf bytes.Buffer
	if err := ssePartialTmpl.Execute(&buf, ev); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// HandleSSEHTML streams task events as SSE with HTML partials
// on the wire. The browser's native EventSource appends each
// data payload directly into the activity feed DOM.
func (h *WebHandler) HandleSSEHTML(
	w http.ResponseWriter, r *http.Request,
) {
	taskID := r.PathValue("id")

	es := h.eventStore()
	if es == nil {
		http.Error(w, "store not configured", http.StatusInternalServerError)
		return
	}

	if _, err := es.GetTask(taskID); err != nil {
		http.NotFound(w, r)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var lastID int64
	if idStr := r.Header.Get("Last-Event-ID"); idStr != "" {
		if parsed, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			lastID = parsed
		}
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			events, err := es.ListEvents(taskID, lastID)
			if err != nil {
				return
			}
			for _, ev := range events {
				html, rerr := renderEventHTML(ev)
				if rerr != nil {
					continue
				}
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.ID, html)
				lastID = ev.ID
			}
			if len(events) > 0 {
				flusher.Flush()
			}

			// Heartbeat.
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()

			// Stop on terminal status.
			task, err := es.GetTask(taskID)
			if err != nil {
				return
			}
			if isTerminalStatus(task.Status) {
				final, err := es.ListEvents(taskID, lastID)
				if err != nil {
					return
				}
				for _, ev := range final {
					html, rerr := renderEventHTML(ev)
					if rerr != nil {
						continue
					}
					fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.ID, html)
				}
				if len(final) > 0 {
					flusher.Flush()
				}
				return
			}
		}
	}
}

// isTerminalStatus reports whether a task status is final.
func isTerminalStatus(status string) bool {
	switch status {
	case "merged", "closed", "failed", "cancelled":
		return true
	}
	return false
}

// eventStore returns the EventStore from h.store, if wired.
func (h *WebHandler) eventStore() EventStore {
	es, _ := h.store.(EventStore)
	return es
}
