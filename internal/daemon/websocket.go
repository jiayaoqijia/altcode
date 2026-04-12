package daemon

import "net/http"

// WSEvent represents a WebSocket event sent to clients.
type WSEvent struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// WSCommand represents a command received from a WebSocket client.
type WSCommand struct {
	Cmd     string `json:"cmd"`
	Message string `json:"message,omitempty"`
	TaskID  string `json:"task_id,omitempty"`
}

// handleWebSocket is a stub endpoint for interactive WebSocket
// connections. WebSocket support requires a dependency addition
// (nhooyr.io/websocket); the SSE endpoint (/tasks/{id}/sse)
// provides equivalent event streaming in the meantime.
func (s *Server) handleWebSocket(
	w http.ResponseWriter, _ *http.Request,
) {
	w.Header().Set("Content-Type", "application/json")
	http.Error(w,
		`{"error":"websocket not yet implemented, use SSE endpoint"}`,
		http.StatusNotImplemented,
	)
}
