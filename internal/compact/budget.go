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
func (c *BudgetCompactor) Apply(messages []provider.Message) []provider.Message {
	totalSize := 0
	for _, m := range messages {
		if m.Role == "tool" {
			totalSize += len(m.Content)
		}
	}

	if totalSize <= c.maxBytes {
		return messages
	}

	result := make([]provider.Message, len(messages))
	copy(result, messages)

	for i := range result {
		if totalSize <= c.maxBytes {
			break
		}
		if result[i].Role == "tool" && len(result[i].Content) > 100 {
			replacement := "[result truncated — exceeded budget]"
			freed := len(result[i].Content) - len(replacement)
			result[i].Content = replacement
			totalSize -= freed
		}
	}

	return result
}
