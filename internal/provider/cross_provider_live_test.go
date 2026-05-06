package provider_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/provider"
)

// Cross-provider live test: same prompt to both Claude and GPT, verify both work.
func TestLive_CrossProvider_SamePrompt(t *testing.T) {
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	openaiKey := os.Getenv("OPENAI_API_KEY")
	openaiBase := os.Getenv("OPENAI_BASE_URL")

	if anthropicKey == "" || openaiKey == "" || openaiBase == "" {
		t.Skip("Need both ANTHROPIC_API_KEY and OPENAI_API_KEY+OPENAI_BASE_URL")
	}

	prompt := "What is the capital of France? Reply with just the city name."

	// Claude
	t.Run("claude", func(t *testing.T) {
		p := provider.NewAnthropic(provider.AnthropicConfig{APIKey: anthropicKey})
		stream, err := p.Stream(context.Background(), &provider.Request{
			Model:     "claude-haiku-4-5-20251001",
			Messages:  []provider.Message{{Role: "user", Content: prompt}},
			MaxTokens: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for ev := range stream {
			if ev.Type == provider.StreamTextDelta {
				text += ev.Delta
			}
		}
		if !strings.Contains(text, "Paris") {
			t.Errorf("Claude: %q", text)
		}
		t.Logf("Claude says: %q", text)
	})

	// GPT
	t.Run("gpt", func(t *testing.T) {
		p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: openaiKey, BaseURL: openaiBase})
		stream, err := p.Stream(context.Background(), &provider.Request{
			Model:     "gpt-5.4",
			Messages:  []provider.Message{{Role: "user", Content: prompt}},
			MaxTokens: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		var text string
		for ev := range stream {
			if ev.Type == provider.StreamTextDelta {
				text += ev.Delta
			}
		}
		if !strings.Contains(text, "Paris") {
			t.Errorf("GPT: %q", text)
		}
		t.Logf("GPT says: %q", text)
	})
}
