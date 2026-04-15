package compact

import "github.com/altcode-ai/altcode/internal/provider"

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
	for i, m := range messages {
		if i < protectFrom && m.Role == "tool" {
			result = append(result, provider.Message{
				Role:    "tool",
				Content: "[previous tool result removed]",
			})
		} else {
			result = append(result, m)
		}
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
