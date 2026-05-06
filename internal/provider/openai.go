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
		client: newStreamingClient(),
	}
}

func (p *openaiProvider) Name() string { return "openai" }

// hasAPIVersionSuffix returns true if the URL ends in a recognised
// API-version path segment (/v1../v9, /api/.../vN). Used to decide
// whether to append /chat/completions vs /v1/chat/completions so
// providers that already include the version in their base URL
// don't double-path.
func hasAPIVersionSuffix(u string) bool {
	for _, suf := range []string{"/v1", "/v2", "/v3", "/v4", "/v5", "/v6", "/v7", "/v8", "/v9"} {
		if strings.HasSuffix(u, suf) {
			return true
		}
	}
	return false
}

func (p *openaiProvider) Stream(ctx context.Context, req *Request) (<-chan StreamEvent, error) {
	body, err := buildOpenAIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// Some providers include the API version in their base URL
	// (moonshot uses /v1, zhipu uses /api/paas/v4, deepseek uses
	// /v1, etc.). When that's the case, append /chat/completions
	// directly instead of /v1/chat/completions to avoid the
	// /v1/v1/... double-path bug. Allow-list of versioned suffixes
	// is more reliable than the previous HasSuffix("/v1")||"/v4"
	// pair, which still missed /v3 (older MiniMax) and other
	// versioned bases.
	completePath := openAICompletePath
	trimmed := strings.TrimRight(p.cfg.BaseURL, "/")
	if hasAPIVersionSuffix(trimmed) {
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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("openai status %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan StreamEvent, 32)
	// Some providers (e.g. altllm) ignore stream:true and return plain JSON.
	// Detect by content-type and convert to synthetic events.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") && !strings.Contains(ct, "event-stream") {
		// Non-streaming JSON path — single ReadAll, no idle window needed.
		go processOpenAINonStream(resp.Body, ch)
	} else {
		// Streaming SSE — wrap with idleReader so half-dead TCP doesn't
		// hang the agent loop forever. 120s no-data is the cutoff.
		go processOpenAISSE(newIdleReader(ctx, resp.Body, 120*time.Second), ch)
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
			InputTokens:  int64(resp.Usage.PromptTokens),
			OutputTokens: int64(resp.Usage.CompletionTokens),
		},
	}
	stopReason := "end_turn"
	if choice.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	} else if choice.FinishReason == "length" {
		stopReason = "max_tokens"
	} else if len(choice.Message.ToolCalls) > 0 {
		// Some OpenAI-compatible providers (altllm, certain Chinese
		// providers) return tool_calls in the response but leave
		// finish_reason blank or set it to "stop". Without this, the
		// engine treats the turn as finished and silently drops every
		// tool call.
		stopReason = "tool_use"
	}
	ch <- StreamEvent{Type: StreamDone, StopReason: stopReason}
}

// --- request types ---

type openaiRequest struct {
	Model              string                `json:"model"`
	Messages           []openaiMessage       `json:"messages"`
	Tools              []openaiTool          `json:"tools,omitempty"`
	MaxTokens          int                   `json:"max_tokens,omitempty"`
	Temperature        *float64              `json:"temperature,omitempty"`
	Stream             bool                  `json:"stream"`
	StreamOptions      *openaiStreamOptions  `json:"stream_options,omitempty"`
	ParallelToolCalls  *bool                 `json:"parallel_tool_calls,omitempty"`
}

// openaiStreamOptions asks the server to include a usage block on the
// final streaming chunk. Without this, chat-completions streams omit
// token counts entirely and the cost tracker stays empty forever.
type openaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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
		// toOpenAIMessages may return multiple openai messages for one
		// altcode message — a user message containing N tool_result
		// parts becomes N OpenAI tool messages. The previous code
		// silently dropped all but the first result and the model
		// then complained about an unmatched tool_call_id.
		msgs = append(msgs, toOpenAIMessages(m)...)
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
		StreamOptions:     &openaiStreamOptions{IncludeUsage: true},
		ParallelToolCalls: &parallel,
	}
	return json.Marshal(r)
}

// toOpenAIMessages converts an altcode Message to one or more OpenAI
// messages. A user message holding N tool_result parts produces N
// OpenAI `role: tool` messages — the previous single-message version
// silently dropped all but the first.
func toOpenAIMessages(m Message) []openaiMessage {
	if len(m.Parts) == 0 {
		return []openaiMessage{{Role: m.Role, Content: m.Content}}
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
		return []openaiMessage{msg}
	}

	// Handle tool_result (user sending back tool results).
	// OpenAI expects role="tool" with tool_call_id, ONE message per
	// result — fan out every tool_result part into its own message.
	var out []openaiMessage
	for _, p := range m.Parts {
		if p.Type == "tool_result" {
			out = append(out, openaiMessage{
				Role:       "tool",
				Content:    p.Content,
				ToolCallID: p.ToolUseID,
			})
		}
	}
	if len(out) > 0 {
		return out
	}

	return []openaiMessage{{Role: m.Role, Content: m.Content}}
}

// toOpenAIMessage is kept as a thin shim for any callers expecting a
// single message; new callers should prefer toOpenAIMessages so they
// don't drop multi-tool-result batches.
func toOpenAIMessage(m Message) openaiMessage {
	msgs := toOpenAIMessages(m)
	if len(msgs) == 0 {
		return openaiMessage{Role: m.Role, Content: m.Content}
	}
	return msgs[0]
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
				InputTokens:  int64(chunk.Usage.PromptTokens),
				OutputTokens: int64(chunk.Usage.CompletionTokens),
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
			// Fire StreamToolCallStart EXACTLY ONCE per tool call.
			// Some OpenAI-compatible providers re-send the function
			// name on subsequent delta chunks for the same index;
			// without the `state.name == ""` guard the engine would
			// see two tool_use blocks with the same id and break
			// downstream accumulation.
			if tc.Function.Name != "" && state.name == "" {
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
			stopReason = normalizeFinishReason(*choice.FinishReason)
		}
	}

	// Some OpenAI-compatible providers return finish_reason="stop"
	// (or no finish_reason at all) on a turn that includes tool_calls.
	// The non-stream path was already fixed; mirror it here so the
	// engine sees stop_reason="tool_use" and dispatches the tools
	// instead of treating the turn as finished.
	if (stopReason == "stop" || stopReason == "") && len(toolState) > 0 {
		stopReason = "tool_use"
	}
	ch <- StreamEvent{Type: StreamDone, StopReason: stopReason}
}

type openaiToolState struct {
	id   string
	name string
	args string
}

// normalizeFinishReason maps OpenAI finish_reason values to the
// Anthropic-style stop reasons used internally by the engine. The
// non-streaming path already normalized these; the streaming path
// forwarded raw values, causing inconsistent StopReason on StreamDone.
func normalizeFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}
