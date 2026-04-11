// Package event defines the core event types used throughout altcode.
package event

import "encoding/json"

// EventType identifies the kind of event emitted by the AI stream.
type EventType string

const (
	TextDelta          EventType = "text_delta"
	TextDone           EventType = "text_done"
	ToolStart          EventType = "tool_start"
	ToolDelta          EventType = "tool_delta"
	ToolDone           EventType = "tool_done"
	ToolResultEvent    EventType = "tool_result"
	ThinkingDelta      EventType = "thinking_delta"
	UsageEvent         EventType = "usage"
	PermissionRequest  EventType = "permission_request"
	PermissionResponse EventType = "permission_response"
	InfoEvent          EventType = "info"
	// BudgetExceeded fires when the engine stops mid-session because
	// a cost/turn/token budget was hit. Event.Info carries a
	// human-readable explanation (e.g. "max-turns 10 reached" or
	// "cost budget $0.50 exceeded ($0.52 used)"). Carriers:
	//   - exec.Params.MaxTurns (Phase 8)
	//   - exec.Params.MaxCost  (Phase 8)
	// Subsequent to this event the engine emits Done and returns.
	BudgetExceeded EventType = "budget_exceeded"
	ErrorEvent     EventType = "error"
	Done           EventType = "done"
)

// Action represents a permission decision.
type Action string

const (
	Allow Action = "allow"
	Deny  Action = "deny"
	Ask   Action = "ask"
)

// ToolCall holds information about a tool invocation.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	Eager bool            `json:"eager,omitempty"`
}

// Result holds the output from a tool execution.
type Result struct {
	Output   string            `json:"output"`
	Title    string            `json:"title,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// UsageInfo tracks token consumption for a request.
type UsageInfo struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheHits    int `json:"cache_hits,omitempty"`
}

// PermResponse carries the result of a permission decision.
type PermResponse struct {
	Action     Action `json:"action"`
	Persistent bool   `json:"persistent,omitempty"`
}

// PermReq describes a permission request for a tool operation.
type PermReq struct {
	ToolName string           `json:"tool_name"`
	Pattern  string           `json:"pattern"`
	Response chan PermResponse `json:"-"`
}

// Event is the unified stream event emitted during an AI turn.
type Event struct {
	Type       EventType    `json:"type"`
	Text       string       `json:"text,omitempty"`
	ToolCall   *ToolCall    `json:"tool_call,omitempty"`
	ToolResult *Result      `json:"tool_result,omitempty"`
	Error      string       `json:"error,omitempty"`
	Usage      *UsageInfo   `json:"usage,omitempty"`
	Thinking   string       `json:"thinking,omitempty"`
	Permission *PermReq     `json:"permission,omitempty"`
	Info       string       `json:"info,omitempty"`
}
