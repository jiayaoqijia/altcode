package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	defaultAnthropicBase    = "https://api.anthropic.com"
	anthropicVersion        = "2023-06-01"
	anthropicMessagesPath   = "/v1/messages"
)

// AnthropicConfig holds credentials and endpoint for the Anthropic provider.
type AnthropicConfig struct {
	APIKey  string
	BaseURL string
}

type anthropicProvider struct {
	cfg    AnthropicConfig
	client *http.Client
}

// NewAnthropic creates a Provider backed by the Anthropic Messages API.
func NewAnthropic(cfg AnthropicConfig) Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultAnthropicBase
	}
	return &anthropicProvider{cfg: cfg, client: newStreamingClient()}
}

func (a *anthropicProvider) Name() string { return "anthropic" }

// Stream opens a streaming request to the Anthropic Messages API and returns
// a channel of StreamEvents. The channel is closed when the stream ends.
func (a *anthropicProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	body, err := buildAnthropicRequest(req)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.BaseURL+anthropicMessagesPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan StreamEvent, 32)
	go processSSE(resp.Body, ch)
	return ch, nil
}

// anthropicRequest is the JSON body sent to /v1/messages.
type anthropicRequest struct {
	Model     string               `json:"model"`
	Messages  []anthropicMessage   `json:"messages"`
	System    []anthropicSystem    `json:"system,omitempty"`
	Tools     []ToolSchema         `json:"tools,omitempty"`
	MaxTokens int                  `json:"max_tokens"`
	Stream    bool                 `json:"stream"`
	Thinking  *ThinkingConfig      `json:"thinking,omitempty"`
	Temperature *float64           `json:"temperature,omitempty"`
	Metadata  map[string]any       `json:"metadata,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []ContentPart
}

type anthropicSystem struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

func buildAnthropicRequest(req *Request) ([]byte, error) {
	msgs := make([]anthropicMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = toAnthropicMessage(m)
	}
	system := make([]anthropicSystem, len(req.System))
	for i, s := range req.System {
		system[i] = anthropicSystem{
			Type:         "text",
			Text:         s.Content,
			CacheControl: s.CacheControl,
		}
	}
	ar := anthropicRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
		Temperature: req.Temperature,
		Thinking:    req.Thinking,
		Metadata:    req.Metadata,
	}
	if len(system) > 0 {
		ar.System = system
	}
	if len(req.Tools) > 0 {
		ar.Tools = req.Tools
	}
	return json.Marshal(ar)
}

func toAnthropicMessage(m Message) anthropicMessage {
	if len(m.Parts) > 0 {
		return anthropicMessage{Role: m.Role, Content: m.Parts}
	}
	return anthropicMessage{Role: m.Role, Content: m.Content}
}

// processSSE reads SSE events from body and sends StreamEvents to ch.
//
// Anthropic's content_block_* events all carry an `index` and content
// blocks may interleave (text + tool_use + text). Track each block by
// its index so deltas and stops route to the right block instead of
// being mis-attributed to whichever tool block happened to start last.
func processSSE(body io.ReadCloser, ch chan<- StreamEvent) {
	defer body.Close()
	defer close(ch)

	decoder := NewSSEDecoder(body)
	blocks := make(map[int]*blockState)
	var stopReason string

	for {
		evtType, data, err := decoder.Next()
		if err != nil {
			if err != io.EOF {
				ch <- StreamEvent{Type: StreamError, Error: err}
			}
			break
		}
		if data == "" {
			continue
		}
		reason, err := dispatchSSEEvent(evtType, data, ch, blocks)
		if err != nil {
			ch <- StreamEvent{Type: StreamError, Error: err}
			break
		}
		if reason != "" {
			stopReason = reason
		}
	}
	ch <- StreamEvent{Type: StreamDone, StopReason: stopReason}
}

// blockState tracks a single content block. Each Anthropic message may
// have several blocks (text + tool_use + text...) and they can interleave.
type blockState struct {
	kind   string // "text", "thinking", "tool_use"
	toolID string
	name   string
}

func dispatchSSEEvent(
	evtType, data string,
	ch chan<- StreamEvent,
	blocks map[int]*blockState,
) (string, error) {
	switch evtType {
	case "content_block_start":
		return "", handleContentBlockStart(data, ch, blocks)
	case "content_block_delta":
		return "", handleContentBlockDelta(data, ch, blocks)
	case "content_block_stop":
		var p struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return "", fmt.Errorf("parse content_block_stop: %w", err)
		}
		bs := blocks[p.Index]
		if bs == nil {
			return "", nil
		}
		switch bs.kind {
		case "tool_use":
			ch <- StreamEvent{Type: StreamToolCallEnd, ToolUse: &ToolCallEvent{
				ID: bs.toolID, Name: bs.name,
			}}
		case "text":
			ch <- StreamEvent{Type: StreamTextDone}
		}
		delete(blocks, p.Index)
	case "message_delta":
		return handleMessageDeltaWithReason(data, ch)
	case "message_stop":
		// nothing to emit; StreamDone is sent by processSSE after loop
	case "error":
		return "", handleErrorEvent(data)
	}
	return "", nil
}

// --- typed SSE payloads ---

type sseContentBlockStart struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
}

type sseContentBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
	} `json:"delta"`
}

type sseMessageDelta struct {
	Delta *struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage *UsageInfo `json:"usage"`
}

type sseError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func handleContentBlockStart(
	data string, ch chan<- StreamEvent, blocks map[int]*blockState,
) error {
	var p sseContentBlockStart
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return fmt.Errorf("parse content_block_start: %w", err)
	}
	bs := &blockState{kind: p.ContentBlock.Type}
	if p.ContentBlock.Type == "tool_use" {
		bs.toolID = p.ContentBlock.ID
		bs.name = p.ContentBlock.Name
		ch <- StreamEvent{Type: StreamToolCallStart, ToolUse: &ToolCallEvent{
			ID: p.ContentBlock.ID, Name: p.ContentBlock.Name,
		}}
	}
	blocks[p.Index] = bs
	return nil
}

func handleContentBlockDelta(
	data string, ch chan<- StreamEvent, blocks map[int]*blockState,
) error {
	var p sseContentBlockDelta
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return fmt.Errorf("parse content_block_delta: %w", err)
	}
	bs := blocks[p.Index]
	switch p.Delta.Type {
	case "text_delta":
		// Only emit if we know the block is text — drops misrouted
		// deltas instead of mixing them into the active text stream.
		if bs == nil || bs.kind == "text" {
			ch <- StreamEvent{Type: StreamTextDelta, Delta: p.Delta.Text}
		}
	case "thinking_delta":
		if bs == nil || bs.kind == "thinking" {
			ch <- StreamEvent{Type: StreamThinkingDelta, Delta: p.Delta.Thinking}
		}
	case "input_json_delta":
		// Look up the tool block by index, not by 'last seen tool'.
		// Previously a tool delta could be silently dropped if a
		// non-tool block had reset the global toolIndex to -1.
		if bs != nil && bs.kind == "tool_use" {
			ch <- StreamEvent{Type: StreamToolCallDelta, ToolUse: &ToolCallEvent{
				ID: bs.toolID, Name: bs.name, Delta: p.Delta.PartialJSON,
			}}
		}
	}
	return nil
}

func handleMessageDeltaWithReason(data string, ch chan<- StreamEvent) (string, error) {
	var p sseMessageDelta
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return "", fmt.Errorf("parse message_delta: %w", err)
	}
	if p.Usage != nil {
		ch <- StreamEvent{Type: StreamUsage, Usage: p.Usage}
	}
	if p.Delta != nil && p.Delta.StopReason != "" {
		return p.Delta.StopReason, nil
	}
	return "", nil
}

func handleErrorEvent(data string) error {
	var p sseError
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return fmt.Errorf("parse error event: %w", err)
	}
	return fmt.Errorf("anthropic error: %s", p.Error.Message)
}
