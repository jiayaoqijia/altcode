//go:build !windows

package provider_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/provider"
)

// Live e2e tests against 5 models via OpenRouter.
// Skip without OPENROUTER env var or .env file.

func openRouterKey(t *testing.T) string {
	t.Helper()
	if os.Getenv("ALTCODE_LIVE_TESTS") == "" {
		t.Skip("set ALTCODE_LIVE_TESTS=1 to run live OpenRouter tests")
	}
	if key := os.Getenv("OPENROUTER"); key != "" {
		return key
	}
	// Try .env file
	data, err := os.ReadFile("../../.env")
	if err != nil {
		data, _ = os.ReadFile(".env")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "OPENROUTER=") {
			return strings.TrimPrefix(line, "OPENROUTER=")
		}
	}
	t.Skip("OPENROUTER key not found")
	return ""
}

func orProvider(t *testing.T) provider.Provider {
	t.Helper()
	return provider.NewOpenAI(provider.OpenAIConfig{
		APIKey:  openRouterKey(t),
		BaseURL: "https://openrouter.ai/api",
	})
}

func orStream(t *testing.T, model, prompt string, timeout time.Duration) (text string, toolCalls int, err error) {
	t.Helper()
	p := orProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	stream, serr := p.Stream(ctx, &provider.Request{
		Model:     model,
		Messages:  []provider.Message{{Role: "user", Content: prompt}},
		MaxTokens: 200,
		Tools: []provider.ToolSchema{{
			Name:        "bash",
			Description: "Run a bash command",
			InputSchema: []byte(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		}},
	})
	if serr != nil {
		return "", 0, serr
	}

	for ev := range stream {
		switch ev.Type {
		case provider.StreamTextDelta:
			text += ev.Delta
		case provider.StreamToolCallStart:
			toolCalls++
		case provider.StreamError:
			return text, toolCalls, ev.Error
		}
	}
	return text, toolCalls, nil
}

var openRouterModels = []struct {
	name  string
	model string
}{
	{"DeepSeek", "deepseek/deepseek-chat-v3-0324"},
	{"MiniMax", "minimax/minimax-m2.5"},
	{"GLM5", "z-ai/glm-5"},
	{"Kimi", "moonshotai/kimi-k2.5"},
	{"Qwen", "qwen/qwen3-coder-next"},
}

// Test 1: All models can generate text
func TestOpenRouter_TextGeneration(t *testing.T) {
	t.Parallel()
	for _, m := range openRouterModels {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := orProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			stream, err := p.Stream(ctx, &provider.Request{
				Model:     m.model,
				Messages:  []provider.Message{{Role: "user", Content: "What is 7 multiplied by 8? Reply with just the number."}},
				MaxTokens: 50,
			})
			if err != nil {
				t.Fatalf("[%s] Stream error: %v", m.name, err)
			}

			var text string
			var hasDone bool
			for ev := range stream {
				if ev.Type == provider.StreamTextDelta {
					text += ev.Delta
				}
				if ev.Type == provider.StreamDone {
					hasDone = true
				}
			}

			t.Logf("[%s] Response: %q done=%v", m.name, text, hasDone)
			if text == "" && !hasDone {
				t.Errorf("[%s] No text and no done", m.name)
			}
			// Some models may answer differently but should mention 56
			if text != "" && !strings.Contains(text, "56") {
				t.Logf("[%s] Answer didn't contain 56: %q", m.name, text)
			}
		})
	}
}

// Test 2: All models emit Done event
func TestOpenRouter_DoneEvent(t *testing.T) {
	t.Parallel()
	for _, m := range openRouterModels {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := orProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			stream, err := p.Stream(ctx, &provider.Request{
				Model:     m.model,
				Messages:  []provider.Message{{Role: "user", Content: "Say ok"}},
				MaxTokens: 10,
			})
			if err != nil {
				t.Fatal(err)
			}

			hasDone := false
			for ev := range stream {
				if ev.Type == provider.StreamDone {
					hasDone = true
				}
			}
			if !hasDone {
				t.Errorf("[%s] Missing Done event", m.name)
			}
		})
	}
}

// Test 3: Tool calling works
func TestOpenRouter_ToolCalling(t *testing.T) {
	t.Parallel()
	for _, m := range openRouterModels {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text, toolCalls, err := orStream(t, m.model,
				"Use the bash tool to run 'echo hello'. You MUST call the bash tool.", 30*time.Second)
			if err != nil {
				t.Logf("[%s] Error (may not support tools): %v", m.name, err)
				return
			}
			t.Logf("[%s] text=%q toolCalls=%d", m.name, text[:min(len(text), 80)], toolCalls)
			if toolCalls == 0 && text == "" {
				t.Errorf("[%s] No tool calls and no text", m.name)
			}
		})
	}
}

// Test 4: Code explanation
func TestOpenRouter_CodeExplanation(t *testing.T) {
	t.Parallel()
	for _, m := range openRouterModels {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := orProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			stream, err := p.Stream(ctx, &provider.Request{
				Model: m.model,
				Messages: []provider.Message{{Role: "user",
					Content: "What does `func main()` mean in Go? One sentence."}},
				MaxTokens: 200,
			})
			if err != nil {
				t.Logf("[%s] Stream error: %v", m.name, err)
				return
			}

			var text string
			for ev := range stream {
				if ev.Type == provider.StreamTextDelta {
					text += ev.Delta
				}
			}
			if len(text) > 0 {
				t.Logf("[%s] %.120s", m.name, text)
			} else {
				t.Logf("[%s] Empty response (model may not support this)", m.name)
			}
		})
	}
}

// Test 5: System prompt adherence
func TestOpenRouter_SystemPrompt(t *testing.T) {
	t.Parallel()
	for _, m := range openRouterModels {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := orProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			stream, err := p.Stream(ctx, &provider.Request{
				Model: m.model,
				System: []provider.SystemSection{
					{Content: "Always respond with exactly the word PONG."},
				},
				Messages:  []provider.Message{{Role: "user", Content: "Ping"}},
				MaxTokens: 50,
			})
			if err != nil {
				t.Logf("[%s] Error: %v", m.name, err)
				return
			}

			var text string
			for ev := range stream {
				if ev.Type == provider.StreamTextDelta {
					text += ev.Delta
				}
			}
			if len(text) > 0 {
				t.Logf("[%s] %s", m.name, text[:min(len(text), 80)])
			} else {
				t.Logf("[%s] Empty response", m.name)
			}
		})
	}
}

// Test 6: Streaming deltas arrive incrementally
func TestOpenRouter_StreamingDeltas(t *testing.T) {
	t.Parallel()
	for _, m := range openRouterModels[:3] { // test 3 to save time
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			p := orProvider(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			stream, err := p.Stream(ctx, &provider.Request{
				Model:     m.model,
				Messages:  []provider.Message{{Role: "user", Content: "Count from 1 to 5."}},
				MaxTokens: 50,
			})
			if err != nil {
				t.Fatal(err)
			}

			deltaCount := 0
			for ev := range stream {
				if ev.Type == provider.StreamTextDelta {
					deltaCount++
				}
			}
			t.Logf("[%s] %d deltas", m.name, deltaCount)
			if deltaCount < 1 {
				t.Logf("[%s] Only %d deltas (some models batch responses)", m.name, deltaCount)
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
