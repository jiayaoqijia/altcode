package compact

import "github.com/altcode-ai/altcode/internal/provider"

// BudgetCompactor truncates old tool results to stay within a byte budget.
type BudgetCompactor struct {
	maxBytes int
}

// NewBudgetCompactor creates a compactor with the given byte budget.
func NewBudgetCompactor(maxBytes int) *BudgetCompactor {
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	return &BudgetCompactor{maxBytes: maxBytes}
}

// Apply truncates tool results from oldest to newest until within budget.
//
// Counts both flat Content (OpenAI tool messages) AND Parts content
// (Anthropic tool_result blocks live in Parts, not Content). The
// previous version saw nothing in the Anthropic path because tool
// result text lives in m.Parts[i].Content / Text, and silently
// returned the bloated message list unchanged.
func (c *BudgetCompactor) Apply(messages []provider.Message) []provider.Message {
	totalSize := 0
	for _, m := range messages {
		totalSize += toolResultBytes(m)
	}

	if totalSize <= c.maxBytes {
		return messages
	}

	result := make([]provider.Message, len(messages))
	copy(result, messages)

	const replacement = "[result truncated — exceeded budget]"
	for i := range result {
		if totalSize <= c.maxBytes {
			break
		}
		size := toolResultBytes(result[i])
		if size <= len(replacement)+64 {
			continue
		}
		// Truncate flat Content for the OpenAI shape.
		if result[i].Role == "tool" && len(result[i].Content) > 100 {
			freed := len(result[i].Content) - len(replacement)
			result[i].Content = replacement
			totalSize -= freed
			continue
		}
		// Truncate tool_result Parts for the Anthropic shape. Copy
		// the slice first so we don't mutate the original messages.
		if hasToolResultPart(result[i]) {
			parts := make([]provider.ContentPart, len(result[i].Parts))
			copy(parts, result[i].Parts)
			freed := 0
			for j := range parts {
				if parts[j].Type != "tool_result" {
					continue
				}
				freed += len(parts[j].Content) + len(parts[j].Text) - len(replacement)
				parts[j].Content = replacement
				parts[j].Text = ""
			}
			result[i].Parts = parts
			totalSize -= freed
		}
	}

	return result
}

// toolResultBytes returns the number of bytes a message contributes
// to the tool-result budget — flat tool Content for OpenAI, Parts
// content for Anthropic.
func toolResultBytes(m provider.Message) int {
	n := 0
	if m.Role == "tool" {
		n += len(m.Content)
	}
	for _, p := range m.Parts {
		if p.Type == "tool_result" {
			n += len(p.Content) + len(p.Text)
		}
	}
	return n
}

func hasToolResultPart(m provider.Message) bool {
	for _, p := range m.Parts {
		if p.Type == "tool_result" {
			return true
		}
	}
	return false
}
