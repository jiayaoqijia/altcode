package orchestra

import (
	"encoding/json"
	"strings"
)

// PhaseEventKind identifies the type of event from a phase.
type PhaseEventKind string

const (
	KindText      PhaseEventKind = "text"
	KindToolStart PhaseEventKind = "tool_start"
	KindToolDone  PhaseEventKind = "tool_done"
	KindThinking  PhaseEventKind = "thinking"
	KindError     PhaseEventKind = "error"
	KindPhaseDone PhaseEventKind = "phase_done"
)

// PhaseEvent is a typed event from a running phase agent.
type PhaseEvent struct {
	Phase     string
	Role      string
	Type      PhaseEventKind
	Text      string
	Tool      string
	SessionID string
}

// ParseClaudeStreamJSON parses one line of claude --output-format stream-json.
func ParseClaudeStreamJSON(line string) *PhaseEvent {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}
	var msgType string
	if t, ok := raw["type"]; ok {
		json.Unmarshal(t, &msgType)
	}

	switch msgType {
	case "assistant":
		return parseAssistantMessage(line)
	case "result":
		var r struct {
			ResultText string `json:"result_text"`
			SessionID  string `json:"session_id"`
		}
		json.Unmarshal([]byte(line), &r)
		return &PhaseEvent{Type: KindPhaseDone, Text: r.ResultText, SessionID: r.SessionID}
	case "system":
		return &PhaseEvent{Type: KindText, Text: "[system]"}
	default:
		return nil
	}
}

func parseAssistantMessage(line string) *PhaseEvent {
	var msg struct {
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				Name  string          `json:"name"`
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &msg) != nil {
		return nil
	}
	for _, block := range msg.Message.Content {
		switch block.Type {
		case "text":
			return &PhaseEvent{Type: KindText, Text: block.Text}
		case "thinking":
			return &PhaseEvent{Type: KindThinking, Text: block.Text}
		case "tool_use":
			return &PhaseEvent{Type: KindToolStart, Tool: block.Name, Text: block.ID}
		case "tool_result":
			return &PhaseEvent{Type: KindToolDone, Tool: block.Name}
		}
	}
	return nil
}

// ParseCodexLine heuristically classifies a raw codex output line.
func ParseCodexLine(line string) *PhaseEvent {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return &PhaseEvent{Type: KindText, Text: ""}
	}
	switch {
	case strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]"):
		end := strings.Index(trimmed, "]")
		tool := trimmed[1:end]
		return &PhaseEvent{Type: KindToolStart, Tool: tool, Text: trimmed}
	case trimmed == "✓" || strings.HasPrefix(trimmed, "✓ "):
		return &PhaseEvent{Type: KindToolDone, Text: trimmed}
	case strings.HasPrefix(strings.ToLower(trimmed), "error"):
		return &PhaseEvent{Type: KindError, Text: trimmed}
	default:
		return &PhaseEvent{Type: KindText, Text: trimmed}
	}
}
