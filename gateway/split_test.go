package gateway

import (
	"strings"
	"testing"
)

func TestSplitMessage_Short(t *testing.T) {
	chunks := SplitMessage("hello", 100)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("expected single chunk, got %v", chunks)
	}
}

func TestSplitMessage_Empty(t *testing.T) {
	chunks := SplitMessage("", 100)
	if chunks != nil {
		t.Fatalf("expected nil, got %v", chunks)
	}
}

func TestSplitMessage_ExactLength(t *testing.T) {
	msg := strings.Repeat("a", 100)
	chunks := SplitMessage(msg, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestSplitMessage_LongMessage(t *testing.T) {
	msg := strings.Repeat("word ", 500) // 2500 chars
	chunks := SplitMessage(msg, 100)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len([]rune(c)) > 100 {
			t.Errorf("chunk exceeds maxLen: %d runes", len([]rune(c)))
		}
	}
}

func TestSplitMessage_ZeroMaxLen(t *testing.T) {
	chunks := SplitMessage("hello", 0)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("expected passthrough, got %v", chunks)
	}
}

func TestSplitMessage_CodeBlock(t *testing.T) {
	msg := "before\n```go\nfunc main() {\n}\n```\nafter"
	chunks := SplitMessage(msg, 1000)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}
