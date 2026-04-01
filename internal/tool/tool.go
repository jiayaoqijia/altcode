package tool

import (
	"context"
	"encoding/json"
)

// Tool is the interface implemented by all tools available to the agent.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, input json.RawMessage) (*Result, error)
	IsConcurrencySafe() bool
	IsReadOnly() bool
	PermissionPattern(input json.RawMessage) string
}

// Result holds the output from a tool execution.
type Result struct {
	Output   string
	Title    string
	Metadata map[string]any
	Error    error
}

// Call represents a pending tool invocation.
type Call struct {
	ID          string
	Tool        Tool
	Input       json.RawMessage
	Eager       bool
	EagerResult *Result
}
