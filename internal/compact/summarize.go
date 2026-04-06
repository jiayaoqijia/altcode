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

	// Call the model to produce the summary
	stream, err := s.provider.Stream(ctx, &provider.Request{
		Model:     s.model,
		Messages:  summaryMessages,
		MaxTokens: 2048,
	})
	if err != nil {
		return messages, fmt.Errorf("compact stream: %w", err)
	}

	var summary strings.Builder
	for ev := range stream {
		if ev.Type == provider.StreamTextDelta {
			summary.WriteString(ev.Delta)
		}
	}

	if summary.Len() == 0 {
		return messages, fmt.Errorf("compact produced empty summary")
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

// EstimateTokens gives a rough token count for the message list.
// Uses the 4-chars-per-token heuristic (same as Codex's approx_token_count).
func EstimateTokens(messages []provider.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
		for _, p := range m.Parts {
			total += len(p.Text) / 4
			total += len(p.Content) / 4
		}
	}
	return total
}
