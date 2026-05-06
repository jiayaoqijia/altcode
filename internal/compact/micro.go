package compact

import "github.com/jiayaoqijia/altcode/internal/provider"

// Microcompactor removes old tool results beyond a turn window.
type Microcompactor struct {
	keepTurns int
}

// NewMicrocompactor creates a compactor that preserves the last N user turns.
func NewMicrocompactor(keepTurns int) *Microcompactor {
	if keepTurns <= 0 {
		keepTurns = 10
	}
	return &Microcompactor{keepTurns: keepTurns}
}

// Apply replaces tool results outside the recent turn window with stubs.
func (c *Microcompactor) Apply(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return messages
	}

	turnCount := 0
	protectFrom := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && !hasToolResultParts(messages[i]) {
			turnCount++
			if turnCount >= c.keepTurns {
				protectFrom = i
				break
			}
		}
	}

	var result []provider.Message
	stub := "[previous tool result removed]"
	for i, m := range messages {
		if i >= protectFrom {
			result = append(result, m)
			continue
		}
		// OpenAI-style: a dedicated role="tool" message. Replace the
		// Content text wholesale.
		if m.Role == "tool" {
			result = append(result, provider.Message{
				Role:    "tool",
				Content: stub,
			})
			continue
		}
		// Anthropic-style: role="user" with one or more `tool_result`
		// parts. Only the tool_result parts are replaced so the
		// surrounding user text (e.g. a real follow-up message that
		// happens to include a tool_result) is preserved. Codex
		// round-H caught that the previous role=="tool"-only check
		// meant Anthropic sessions never benefited from fallback
		// compaction and could overflow.
		if hasToolResultParts(m) {
			newParts := make([]provider.ContentPart, 0, len(m.Parts))
			for _, p := range m.Parts {
				if p.Type == "tool_result" {
					newParts = append(newParts, provider.ContentPart{
						Type:      "tool_result",
						ToolUseID: p.ToolUseID,
						Content:   stub,
					})
				} else {
					newParts = append(newParts, p)
				}
			}
			result = append(result, provider.Message{
				Role:    m.Role,
				Content: m.Content,
				Parts:   newParts,
			})
			continue
		}
		result = append(result, m)
	}

	return result
}

// hasToolResultParts checks if a message contains tool_result content parts.
// Anthropic stores tool results as role="user" + tool_result parts, which
// should NOT count as user turns for compaction purposes.
func hasToolResultParts(m provider.Message) bool {
	for _, p := range m.Parts {
		if p.Type == "tool_result" {
			return true
		}
	}
	return false
}
