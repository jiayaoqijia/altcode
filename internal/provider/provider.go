// Package provider defines the provider abstraction layer for AI model streaming.
package provider

import (
	"context"
	"encoding/json"
)

// Provider is the interface implemented by all AI model backends.
type Provider interface {
	Name() string
	Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error)
}

// Request holds all parameters for a model inference call.
type Request struct {
	Model       string
	Messages    []Message
	System      []SystemSection
	Tools       []ToolSchema
	MaxTokens   int
	Temperature *float64
	Thinking    *ThinkingConfig
	Metadata    map[string]any
}

// SystemSection is a system prompt block, optionally with cache control.
type SystemSection struct {
	Content      string
	CacheControl *CacheControl
}

// CacheControl specifies prompt caching behavior.
type CacheControl struct {
	Type string `json:"type"`
}

// ToolSchema describes a tool available to the model.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ThinkingConfig enables extended thinking mode.
type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// StreamEventType identifies the kind of event emitted by the provider stream.
type StreamEventType int

const (
	StreamTextDelta     StreamEventType = iota
	StreamTextDone
	StreamToolCallStart
	StreamToolCallDelta
	StreamToolCallEnd
	StreamThinkingDelta
	StreamUsage
	StreamError
	StreamDone
)

// StreamEvent is a single event emitted from a provider stream.
type StreamEvent struct {
	Type       StreamEventType
	Delta      string
	ToolUse    *ToolCallEvent
	Usage      *UsageInfo
	Error      error
	StopReason string // "end_turn", "max_tokens", "stop", "length", etc.
}

// ToolCallEvent carries information about a streaming tool call.
type ToolCallEvent struct {
	ID    string
	Name  string
	Delta string
}

// UsageInfo records token consumption for a request.
//
// CacheCreationInputTokens / CacheReadInputTokens are Anthropic-only
// fields that describe prompt caching cost. Without them the cost
// tracker undercounts cached prompts substantially because it sees
// only the (small) uncached `input_tokens` field on message_delta.
type UsageInfo struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}
