package engine

import (
	"encoding/json"
	"strings"

	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/provider"
)

// collectedToolCall holds a fully accumulated tool call from a stream.
type collectedToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// turnResult holds the complete output from a single provider stream.
type turnResult struct {
	Text      string
	ToolCalls []collectedToolCall
}

// collectTurn reads a provider stream, emits events to out in real-time,
// and returns the accumulated text and tool calls.
func collectTurn(stream <-chan provider.StreamEvent, out chan<- event.Event) *turnResult {
	result := &turnResult{}
	var currentTool *toolAccumulator

	for sev := range stream {
		switch sev.Type {
		case provider.StreamTextDelta:
			result.Text += sev.Delta
			out <- event.Event{Type: event.TextDelta, Text: sev.Delta}

		case provider.StreamTextDone:
			out <- event.Event{Type: event.TextDone, Text: result.Text}

		case provider.StreamThinkingDelta:
			out <- event.Event{Type: event.ThinkingDelta, Thinking: sev.Delta}

		case provider.StreamToolCallStart:
			currentTool = &toolAccumulator{
				id:   sev.ToolUse.ID,
				name: sev.ToolUse.Name,
			}
			out <- event.Event{
				Type:     event.ToolStart,
				ToolCall: &event.ToolCall{ID: sev.ToolUse.ID, Name: sev.ToolUse.Name},
			}

		case provider.StreamToolCallDelta:
			if currentTool != nil {
				currentTool.buf.WriteString(sev.ToolUse.Delta)
			}
			out <- event.Event{
				Type:     event.ToolDelta,
				ToolCall: &event.ToolCall{ID: sev.ToolUse.ID, Name: sev.ToolUse.Name},
				Text:     sev.ToolUse.Delta,
			}

		case provider.StreamToolCallEnd:
			if currentTool != nil {
				result.ToolCalls = append(result.ToolCalls, currentTool.finish())
				out <- event.Event{
					Type:     event.ToolDone,
					ToolCall: &event.ToolCall{ID: currentTool.id, Name: currentTool.name},
				}
				currentTool = nil
			}

		case provider.StreamUsage:
			if sev.Usage != nil {
				out <- event.Event{Type: event.UsageEvent, Usage: &event.UsageInfo{
					InputTokens:  sev.Usage.InputTokens,
					OutputTokens: sev.Usage.OutputTokens,
				}}
			}

		case provider.StreamError:
			out <- event.Event{Type: event.ErrorEvent, Error: sev.Error.Error()}
			return result

		case provider.StreamDone:
			// Stream complete — return what we have
			return result
		}
	}
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
