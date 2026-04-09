package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOpenAIBase  = "https://api.openai.com"
	openAICompletePath = "/v1/chat/completions"
)

// OpenAIConfig holds credentials for any OpenAI-compatible provider.
type OpenAIConfig struct {
	APIKey  string
	BaseURL string // override for Ollama, LMStudio, etc.
}

type openaiProvider struct {
	cfg    OpenAIConfig
	client *http.Client
}

// NewOpenAI creates a Provider for OpenAI, Codex, or any compatible API.
func NewOpenAI(cfg OpenAIConfig) Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultOpenAIBase
	}
	return &openaiProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (p *openaiProvider) Name() string { return "openai" }

func (p *openaiProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	body, err := buildOpenAIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// Some providers include /v1 in their base URL (moonshot, zhipu).
	// Detect and use /chat/completions instead of /v1/chat/completions to
	// avoid the /v1/v1/... double-path bug.
	completePath := openAICompletePath
	trimmed := strings.TrimRight(p.cfg.BaseURL, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.HasSuffix(trimmed, "/v4") {
		completePath = "/chat/completions"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		trimmed+completePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openai status %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan StreamEvent, 32)
	// Some providers (e.g. altllm) ignore stream:true and return plain JSON.
	// Detect by content-type and convert to synthetic events.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") && !strings.Contains(ct, "event-stream") {
		go processOpenAINonStream(resp.Body, ch)
	} else {
		go processOpenAISSE(resp.Body, ch)
	}
	return ch, nil
}

// processOpenAINonStream handles providers that return a full JSON response
// instead of SSE. Converts the single response into synthetic stream events.
func processOpenAINonStream(body io.ReadCloser, ch chan<- StreamEvent) {
	defer body.Close()
	defer close(ch)

	data, err := io.ReadAll(body)
	if err != nil {
		ch <- StreamEvent{Type: StreamError, Error: err}
		return
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content   string           `json:"content"`
				ToolCalls []openaiToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		ch <- StreamEvent{Type: StreamError, Error: fmt.Errorf("parse json: %w", err)}
		return
	}
	if len(resp.Choices) == 0 {
		ch <- StreamEvent{Type: StreamDone}
		return
	}

	choice := resp.Choices[0]
	if choice.Message.Content != "" {
		ch <- StreamEvent{Type: StreamTextDelta, Delta: choice.Message.Content}
	}
	for _, tc := range choice.Message.ToolCalls {
		ch <- StreamEvent{
			Type:    StreamToolCallStart,
			ToolUse: &ToolCallEvent{ID: tc.ID, Name: tc.Function.Name},
		}
		ch <- StreamEvent{
			Type:    StreamToolCallDelta,
			ToolUse: &ToolCallEvent{ID: tc.ID, Delta: tc.Function.Arguments},
		}
		ch <- StreamEvent{
			Type:    StreamToolCallEnd,
			ToolUse: &ToolCallEvent{ID: tc.ID},
		}
	}
	ch <- StreamEvent{
		Type: StreamUsage,
		Usage: &UsageInfo{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}
	stopReason := "end_turn"
	if choice.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	} else if choice.FinishReason == "length" {
		stopReason = "max_tokens"
	}
	ch <- StreamEvent{Type: StreamDone, StopReason: stopReason}
}

// --- request types ---

type openaiRequest struct {
	Model              string          `json:"model"`
	Messages           []openaiMessage `json:"messages"`
	Tools              []openaiTool    `json:"tools,omitempty"`
	MaxTokens          int             `json:"max_tokens,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	Stream             bool            `json:"stream"`
	ParallelToolCalls  *bool           `json:"parallel_tool_calls,omitempty"`
}

type openaiMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func buildOpenAIRequest(req *Request) ([]byte, error) {
	msgs := make([]openaiMessage, 0, len(req.Messages)+1)

	// System message from system sections
	if len(req.System) > 0 {
		var sysContent string
		for _, s := range req.System {
			sysContent += s.Content + "\n\n"
		}
		msgs = append(msgs, openaiMessage{Role: "system", Content: sysContent})
	}

	for _, m := range req.Messages {
		msgs = append(msgs, toOpenAIMessage(m))
	}

	var tools []openaiTool
	for _, t := range req.Tools {
		tools = append(tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	parallel := true
	r := openaiRequest{
		Model:             req.Model,
		Messages:          msgs,
		Tools:             tools,
		MaxTokens:         req.MaxTokens,
		Temperature:       req.Temperature,
		Stream:            true,
		ParallelToolCalls: &parallel,
	}
	return json.Marshal(r)
}

func toOpenAIMessage(m Message) openaiMessage {
	if len(m.Parts) == 0 {
		return openaiMessage{Role: m.Role, Content: m.Content}
	}

	// Handle tool_use (assistant with tool calls)
	if m.Role == "assistant" {
		msg := openaiMessage{Role: "assistant"}
		var toolCalls []openaiToolCall
		for _, p := range m.Parts {
			switch p.Type {
			case "text":
				msg.Content = p.Text
			case "tool_use":
				toolCalls = append(toolCalls, openaiToolCall{
					ID:   p.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: p.Name, Arguments: string(p.Input)},
				})
			}
		}
		msg.ToolCalls = toolCalls
		return msg
	}

	// Handle tool_result (user sending back tool results)
	// OpenAI expects role="tool" with tool_call_id, one message per result
	// But we batch — return the first result; the engine should split these
	for _, p := range m.Parts {
		if p.Type == "tool_result" {
			return openaiMessage{
				Role:       "tool",
				Content:    p.Content,
				ToolCallID: p.ToolUseID,
			}
		}
	}

	return openaiMessage{Role: m.Role, Content: m.Content}
}

// --- SSE processing ---

type openaiSSEChunk struct {
	Choices []struct {
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []openaiSSEDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openaiSSEDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

func processOpenAISSE(body io.ReadCloser, ch chan<- StreamEvent) {
	defer body.Close()
	defer close(ch)

	decoder := NewSSEDecoder(body)
	toolState := make(map[int]*openaiToolState)
	var stopReason string

	for {
		_, data, err := decoder.Next()
		if err != nil {
			if err != io.EOF {
				ch <- StreamEvent{Type: StreamError, Error: err}
			}
			break
		}
		if data == "" || data == "[DONE]" {
			break
		}

		var chunk openaiSSEChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Usage can appear on any chunk (often alongside finish_reason)
		if chunk.Usage != nil {
			ch <- StreamEvent{Type: StreamUsage, Usage: &UsageInfo{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Text content
		if choice.Delta.Content != "" {
			ch <- StreamEvent{Type: StreamTextDelta, Delta: choice.Delta.Content}
		}

		// Tool calls
		for _, tc := range choice.Delta.ToolCalls {
			state, exists := toolState[tc.Index]
			if !exists {
				state = &openaiToolState{}
				toolState[tc.Index] = state
			}
			if tc.ID != "" {
				state.id = tc.ID
			}
			if tc.Function.Name != "" {
				state.name = tc.Function.Name
				ch <- StreamEvent{Type: StreamToolCallStart, ToolUse: &ToolCallEvent{
					ID: state.id, Name: state.name,
				}}
			}
			if tc.Function.Arguments != "" {
				state.args += tc.Function.Arguments
				ch <- StreamEvent{Type: StreamToolCallDelta, ToolUse: &ToolCallEvent{
					ID: state.id, Name: state.name, Delta: tc.Function.Arguments,
				}}
			}
		}

		// Finish
		if choice.FinishReason != nil {
			// Emit tool call end events
			for _, state := range toolState {
				if state.name != "" {
					ch <- StreamEvent{Type: StreamToolCallEnd, ToolUse: &ToolCallEvent{
						ID: state.id, Name: state.name,
					}}
				}
			}
			if *choice.FinishReason == "stop" {
				ch <- StreamEvent{Type: StreamTextDone}
			}
			stopReason = *choice.FinishReason
		}
	}

	ch <- StreamEvent{Type: StreamDone, StopReason: stopReason}
}

type openaiToolState struct {
	id   string
	name string
	args string
}
