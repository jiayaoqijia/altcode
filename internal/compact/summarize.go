package compact

import (
	"context"
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/provider"
)

// SummarizationPrompt is sent to the model to create a handoff summary.
// Matches Codex's compact/prompt.md template.
const SummarizationPrompt = `You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly continue the work.`

// SummaryPrefix is prepended to the conversation after compaction.
const SummaryPrefix = `Another language model started to solve this problem and produced a summary of its thinking process. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary:
`

// Summarizer uses the LLM to create a context-preserving summary
// of the conversation, then replaces the history with it.
type Summarizer struct {
	provider provider.Provider
	model    string
}

// NewSummarizer creates a summarizer backed by the given provider.
func NewSummarizer(p provider.Provider, model string) *Summarizer {
	return &Summarizer{provider: p, model: model}
}

// Compact asks the model to summarize the conversation so far,
// then returns a minimal message list: [system context, summary, recent turns].
// keepRecent controls how many recent user turns to preserve verbatim.
func (s *Summarizer) Compact(ctx context.Context, messages []provider.Message, keepRecent int) ([]provider.Message, error) {
	if len(messages) <= keepRecent*2+2 {
		// Too short to compact
		return messages, nil
	}

	// Split: old messages to summarize vs recent to keep
	cutoff := len(messages) - keepRecent*2
	if cutoff < 2 {
		cutoff = 2
	}
	old := messages[:cutoff]
	recent := messages[cutoff:]

	// Build summary request: old conversation + summarization prompt
	summaryMessages := make([]provider.Message, len(old)+1)
	copy(summaryMessages, old)
	summaryMessages[len(old)] = provider.TextMessage("user", SummarizationPrompt)

	// Call the model to produce the summary — retry with trimming on overflow
	var summary strings.Builder
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		summary.Reset()
		stream, err := s.provider.Stream(ctx, &provider.Request{
			Model:     s.model,
			Messages:  summaryMessages,
			MaxTokens: 2048,
		})
		if err != nil {
			// If context overflow, trim oldest messages and retry
			if isOverflow(err.Error()) && len(summaryMessages) > 4 {
				summaryMessages = trimOldest(summaryMessages, len(summaryMessages)/3)
				continue
			}
			return messages, fmt.Errorf("compact stream: %w", err)
		}
		for ev := range stream {
			if ev.Type == provider.StreamTextDelta {
				summary.WriteString(ev.Delta)
			}
		}
		if summary.Len() > 0 {
			break
		}
	}

	if summary.Len() == 0 {
		return messages, fmt.Errorf("compact produced empty summary after %d attempts", maxRetries)
	}

	// Build compacted history:
	// 1. Preserve any system/initial context from the beginning of the conversation
	// 2. Summary as context handoff
	// 3. Recent turns verbatim
	compacted := make([]provider.Message, 0, len(recent)+4)

	// Reinject initial context: keep first system message if present
	// (like Codex's insert_initial_context_before_last_real_user_or_summary)
	if len(old) > 0 && old[0].Role == "system" {
		compacted = append(compacted, old[0])
	}

	compacted = append(compacted, provider.TextMessage("user", SummaryPrefix))
	compacted = append(compacted, provider.TextMessage("assistant", summary.String()))
	compacted = append(compacted, recent...)

	return compacted, nil
}

// isOverflow checks if an error indicates context length exceeded.
func isOverflow(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "context_length") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "too many tokens") ||
		strings.Contains(lower, "token limit") ||
		strings.Contains(lower, "request too large")
}

// trimOldest removes the first n messages (preserving the last ones).
func trimOldest(messages []provider.Message, n int) []provider.Message {
	if n >= len(messages)-1 {
		n = len(messages) / 2
	}
	return messages[n:]
}

// EstimateTokens gives a conservative token count for the message list.
// Uses 4-chars-per-token heuristic with 4/3 padding multiplier
// (same conservative approach as OpenHarness's TOKEN_ESTIMATION_PADDING).
func EstimateTokens(messages []provider.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
		for _, p := range m.Parts {
			total += len(p.Text) / 4
			total += len(p.Content) / 4
		}
	}
	// Conservative padding: 4/3 multiplier to avoid underestimation
	return total * 4 / 3
}
