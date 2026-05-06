package provider_test

import (
	"context"
	"os"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/provider"
)

func TestAnthropicStream(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	p := provider.NewAnthropic(provider.AnthropicConfig{APIKey: key})
	req := &provider.Request{
		Model: "claude-haiku-4-5-20251001",
		Messages: []provider.Message{
			{Role: "user", Content: "Say hello in exactly 3 words."},
		},
		System:    []provider.SystemSection{{Content: "You are a helpful assistant."}},
		MaxTokens: 100,
	}

	stream, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var gotDone bool
	for evt := range stream {
		switch evt.Type {
		case provider.StreamError:
			t.Fatalf("stream error: %v", evt.Error)
		case provider.StreamTextDelta:
			text += evt.Delta
		case provider.StreamDone:
			gotDone = true
		}
	}

	if text == "" {
		t.Error("expected non-empty text response")
	}
	if !gotDone {
		t.Error("expected StreamDone event")
	}
	t.Logf("response: %q", text)
}
