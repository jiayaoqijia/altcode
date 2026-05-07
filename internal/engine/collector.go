package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/event"
	"github.com/jiayaoqijia/altcode/internal/provider"
)

// collectedToolCall holds a fully accumulated tool call from a stream.
type collectedToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// turnResult holds the complete output from a single provider stream.
type turnResult struct {
	Text                string
	ToolCalls           []collectedToolCall
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64 // Anthropic prompt-cache write count
	CacheReadTokens     int64 // Anthropic prompt-cache hit count
	Truncated           bool  // true when finish_reason indicates max_tokens/length
}

// collectTurn reads a provider stream, emits events to out in real-time,
// and returns the accumulated text and tool calls.
//
// Parallel tool calls: providers stream multiple tool calls interleaved,
// tagged by ID (OpenAI) or block index (Anthropic). We track every active
// accumulator by ID so interleaved start/delta events don't trample each
// other. Start events that re-arrive for an already-active ID (some OpenAI
// responses repeat the name chunk) are deduped so the TUI only sees one
// ToolStart per tool call.
//
// All sends use sendEvent(ctx, ...) so the collector can't deadlock when
// the consumer (TUI) stops draining mid-stream. Without ctx-aware sends
// the collector goroutine blocks on a full channel forever and the
// engine's outer loop can never reach its deferred Done.
func collectTurn(ctx context.Context, stream <-chan provider.StreamEvent, out chan<- event.Event) *turnResult {
	result := &turnResult{}
	active := map[string]*toolAccumulator{}
	// Order preserved so ToolCalls come back in the order the model emitted.
	var order []string

	finalize := func(id string) {
		acc, ok := active[id]
		if !ok {
			return
		}
		result.ToolCalls = append(result.ToolCalls, acc.finish())
		delete(active, id)
		sendEvent(ctx, out, event.Event{
			Type:     event.ToolDone,
			ToolCall: &event.ToolCall{ID: acc.id, Name: acc.name},
		})
	}

	flushAll := func() {
		for _, id := range order {
			finalize(id)
		}
		order = order[:0]
	}

	for sev := range stream {
		switch sev.Type {
		case provider.StreamTextDelta:
			result.Text += sev.Delta
			sendEvent(ctx, out, event.Event{Type: event.TextDelta, Text: sev.Delta})

		case provider.StreamTextDone:
			sendEvent(ctx, out, event.Event{Type: event.TextDone, Text: result.Text})

		case provider.StreamThinkingDelta:
			sendEvent(ctx, out, event.Event{Type: event.ThinkingDelta, Thinking: sev.Delta})

		case provider.StreamToolCallStart:
			id := sev.ToolUse.ID
			if _, seen := active[id]; seen {
				// Dedupe repeat Start (OpenAI sometimes re-sends name chunks).
				continue
			}
			active[id] = &toolAccumulator{
				id:   sev.ToolUse.ID,
				name: sev.ToolUse.Name,
			}
			order = append(order, id)
			sendEvent(ctx, out, event.Event{
				Type:     event.ToolStart,
				ToolCall: &event.ToolCall{ID: sev.ToolUse.ID, Name: sev.ToolUse.Name},
			})

		case provider.StreamToolCallDelta:
			if acc, ok := active[sev.ToolUse.ID]; ok {
				acc.buf.WriteString(sev.ToolUse.Delta)
			}
			sendEvent(ctx, out, event.Event{
				Type:     event.ToolDelta,
				ToolCall: &event.ToolCall{ID: sev.ToolUse.ID, Name: sev.ToolUse.Name},
				Text:     sev.ToolUse.Delta,
			})

		case provider.StreamToolCallEnd:
			finalize(sev.ToolUse.ID)

		case provider.StreamUsage:
			if sev.Usage != nil {
				// Anthropic emits InputTokens on message_start and
				// OutputTokens on message_delta in two separate events.
				// Accumulate so the final result has the latest of each
				// non-zero value rather than overwriting with a zero
				// from the partial event.
				if sev.Usage.InputTokens > 0 {
					result.InputTokens = sev.Usage.InputTokens
				}
				if sev.Usage.OutputTokens > 0 {
					result.OutputTokens = sev.Usage.OutputTokens
				}
				if sev.Usage.CacheCreationInputTokens > 0 {
					result.CacheCreationTokens = sev.Usage.CacheCreationInputTokens
				}
				if sev.Usage.CacheReadInputTokens > 0 {
					result.CacheReadTokens = sev.Usage.CacheReadInputTokens
				}
				sendEvent(ctx, out, event.Event{Type: event.UsageEvent, Usage: &event.UsageInfo{
					InputTokens:  sev.Usage.InputTokens,
					OutputTokens: sev.Usage.OutputTokens,
					CacheHits:    sev.Usage.CacheReadInputTokens,
				}})
			}

		case provider.StreamError:
			flushAll()
			// Don't surface a user-initiated cancel as an error. The
			// TUI already writes '[cancelled]' when it cancels.
			if sev.Error != nil && !errors.Is(sev.Error, context.Canceled) {
				sendEvent(ctx, out, event.Event{Type: event.ErrorEvent, Error: sev.Error.Error()})
			}
			return result

		case provider.StreamDone:
			// Finalize any tool calls that never got explicit End events
			// (some providers emit tool calls without a matching End).
			flushAll()
			result.Truncated = isTruncated(sev.StopReason)
			return result
		}
	}
	// Stream closed without StreamDone — still flush any active tools.
	flushAll()
	return result
}

// toolAccumulator collects incremental JSON deltas for a single tool call.
type toolAccumulator struct {
	id   string
	name string
	buf  strings.Builder
}

func (a *toolAccumulator) finish() collectedToolCall {
	raw := a.buf.String()
	if raw == "" {
		raw = "{}"
	}
	return collectedToolCall{
		ID:    a.id,
		Name:  a.name,
		Input: json.RawMessage(raw),
	}
}

// isTruncated returns true when the stop reason indicates the model
// hit max_tokens (Anthropic) or length (OpenAI).
func isTruncated(reason string) bool {
	return reason == "max_tokens" || reason == "length"
}
