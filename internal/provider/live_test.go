package provider_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/altcode-ai/altcode/internal/provider"
)

// Live integration tests — only run when API keys are set.
// These tests hit real APIs and cost real money.

func TestLive_OpenAICodexRelay(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	p := provider.NewOpenAI(provider.OpenAIConfig{
		APIKey:  key,
		BaseURL: baseURL,
	})

	req := &provider.Request{
		Model: "gpt-5.4",
		Messages: []provider.Message{
			{Role: "user", Content: "What is 2+2? Reply with just the number."},
		},
		MaxTokens: 10,
	}

	stream, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var text string
	var gotDone bool
	for ev := range stream {
		switch ev.Type {
		case provider.StreamTextDelta:
			text += ev.Delta
		case provider.StreamDone:
			gotDone = true
		case provider.StreamError:
			t.Fatalf("Error: %v", ev.Error)
		}
	}

	if text == "" {
		t.Error("Expected non-empty response")
	}
	if !gotDone {
		t.Error("Expected Done")
	}
	t.Logf("Response: %q", text)
}

func TestLive_OpenAIWithToolSchema(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	p := provider.NewOpenAI(provider.OpenAIConfig{
		APIKey:  key,
		BaseURL: baseURL,
	})

	req := &provider.Request{
		Model: "gpt-5.4",
		Messages: []provider.Message{
			{Role: "user", Content: "List files in the current directory using the ls tool."},
		},
		Tools: []provider.ToolSchema{{
			Name:        "ls",
			Description: "List directory contents",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		}},
		MaxTokens: 200,
	}

	stream, err := p.Stream(context.Background(), req)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotToolCall bool
	for ev := range stream {
		if ev.Type == provider.StreamToolCallStart {
			gotToolCall = true
			t.Logf("Tool call: %s (%s)", ev.ToolUse.Name, ev.ToolUse.ID)
		}
		if ev.Type == provider.StreamError {
			t.Fatalf("Error: %v", ev.Error)
		}
	}

	if !gotToolCall {
		t.Error("Expected model to call the ls tool")
	}
}

func TestLive_AnthropicStream(t *testing.T) {
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
	for ev := range stream {
		if ev.Type == provider.StreamTextDelta {
			text += ev.Delta
		}
		if ev.Type == provider.StreamError {
			t.Fatalf("Error: %v", ev.Error)
		}
	}

	if text == "" {
		t.Error("Expected non-empty response")
	}
	t.Logf("Response: %q", text)
}
