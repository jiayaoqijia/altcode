package compact_test

import (
	"testing"

	"github.com/altcode-ai/altcode/internal/compact"
	"github.com/altcode-ai/altcode/internal/provider"
)

func TestToolResultBudget(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: "read files"},
		{Role: "assistant", Content: "I'll read them."},
		{Role: "tool", Content: makeString(100_000)},
		{Role: "assistant", Content: "Found it."},
		{Role: "user", Content: "read more"},
		{Role: "assistant", Content: "Reading."},
		{Role: "tool", Content: makeString(100_000)},
		{Role: "assistant", Content: "Done."},
		{Role: "user", Content: "and more"},
		{Role: "assistant", Content: "Sure."},
		{Role: "tool", Content: makeString(100_000)},
	}

	compactor := compact.NewBudgetCompactor(200_000)
	compacted := compactor.Apply(messages)

	totalSize := 0
	for _, m := range compacted {
		if m.Role == "tool" {
			totalSize += len(m.Content)
		}
	}

	if totalSize > 200_000 {
		t.Fatalf("Expected total tool output <= 200KB, got %d", totalSize)
	}
}

func TestMicrocompact(t *testing.T) {
	messages := []provider.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "tool_use:read"},
		{Role: "tool", Content: "file contents"},
		{Role: "assistant", Content: "I see the file."},
	}

	for i := 0; i < 15; i++ {
		messages = append(messages,
			provider.Message{Role: "user", Content: "next"},
			provider.Message{Role: "assistant", Content: "tool_use:read"},
			provider.Message{Role: "tool", Content: "more content"},
			provider.Message{Role: "assistant", Content: "processed"},
		)
	}

	mc := compact.NewMicrocompactor(10)
	compacted := mc.Apply(messages)

	if len(compacted) != len(messages) {
		t.Fatalf("Expected same length, got %d vs %d", len(compacted), len(messages))
	}

	// Verify early tool results were replaced
	stubCount := 0
	for _, m := range compacted {
		if m.Content == "[previous tool result removed]" {
			stubCount++
		}
	}
	if stubCount == 0 {
		t.Fatal("Expected some tool results to be replaced")
	}
}

func makeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

// Regression for the negative keepRecent panic GPT flagged: the old
// cutoff math produced messages[:len+|n|] and panicked with slice
// bounds out of range. Use a short enough conversation that the
// 'too short to compact' early-exit fires — so we don't reach the
// nil provider's Stream method and test only the cutoff math.
func TestSummarizerCompact_NegativeKeepRecentNoPanic(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	s := compact.NewSummarizer(nil, "")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Compact panicked on negative keepRecent: %v", r)
		}
	}()
	_, _ = s.Compact(nil, msgs, -5)
	_, _ = s.Compact(nil, msgs, 0)
	_, _ = s.Compact(nil, msgs, -2147483648)
}
