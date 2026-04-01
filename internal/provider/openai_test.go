package provider_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/provider"
)

func openaiSSE(data string) string {
	return fmt.Sprintf("data: %s\n\n", data)
}

func TestOpenAI_TextStream(t *testing.T) {
	body := openaiSSE(`{"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`) +
		openaiSSE(`{"choices":[{"delta":{"content":" world"},"finish_reason":null}]}`) +
		openaiSSE(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`) +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("Missing Bearer token")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, err := p.Stream(context.Background(), &provider.Request{
		Model:     "gpt-4",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
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

	if text != "Hello world" {
		t.Errorf("Expected 'Hello world', got %q", text)
	}
	if !gotDone {
		t.Error("Missing Done")
	}
}

func TestOpenAI_ToolCallStream(t *testing.T) {
	body := openaiSSE(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]},"finish_reason":null}]}`) +
		openaiSSE(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_path\":\"/tmp/x\"}"}}]},"finish_reason":null}]}`) +
		openaiSSE(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`) +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, err := p.Stream(context.Background(), &provider.Request{
		Model:     "gpt-4",
		Messages:  []provider.Message{{Role: "user", Content: "read /tmp/x"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotStart, gotDelta, gotEnd bool
	var toolName, toolDelta string
	for ev := range stream {
		switch ev.Type {
		case provider.StreamToolCallStart:
			gotStart = true
			toolName = ev.ToolUse.Name
		case provider.StreamToolCallDelta:
			gotDelta = true
			toolDelta += ev.ToolUse.Delta
		case provider.StreamToolCallEnd:
			gotEnd = true
		}
	}

	if !gotStart {
		t.Error("Missing ToolCallStart")
	}
	if !gotDelta {
		t.Error("Missing ToolCallDelta")
	}
	if !gotEnd {
		t.Error("Missing ToolCallEnd")
	}
	if toolName != "read" {
		t.Errorf("Tool name: %q", toolName)
	}
	if !strings.Contains(toolDelta, "file_path") {
		t.Errorf("Tool delta: %q", toolDelta)
	}
}

func TestOpenAI_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	_, err := p.Stream(context.Background(), &provider.Request{
		Model:     "gpt-4",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err == nil {
		t.Fatal("Expected error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("Error should contain status: %v", err)
	}
}

func TestOpenAI_UsageEvent(t *testing.T) {
	body := openaiSSE(`{"choices":[{"delta":{"content":"hi"},"finish_reason":null}]}`) +
		openaiSSE(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":15,"completion_tokens":3}}`) +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, _ := p.Stream(context.Background(), &provider.Request{
		Model:     "gpt-4",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})

	var gotUsage bool
	for ev := range stream {
		if ev.Type == provider.StreamUsage && ev.Usage != nil {
			gotUsage = true
			if ev.Usage.InputTokens != 15 || ev.Usage.OutputTokens != 3 {
				t.Errorf("Usage wrong: %+v", ev.Usage)
			}
		}
	}
	if !gotUsage {
		t.Error("Missing usage event")
	}
}

func TestOpenAI_EmptyResponse(t *testing.T) {
	body := "data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test", BaseURL: srv.URL})
	stream, _ := p.Stream(context.Background(), &provider.Request{
		Model:     "gpt-4",
		Messages:  []provider.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})

	var gotDone bool
	for ev := range stream {
		if ev.Type == provider.StreamDone {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("Should get Done even on empty response")
	}
}

func TestOpenAI_ProviderName(t *testing.T) {
	p := provider.NewOpenAI(provider.OpenAIConfig{APIKey: "test"})
	if p.Name() != "openai" {
		t.Errorf("Name: %q", p.Name())
	}
}

func TestOpenAI_DefaultBaseURL(t *testing.T) {
	// Just verify it doesn't panic with empty config
	p := provider.NewOpenAI(provider.OpenAIConfig{})
	if p.Name() != "openai" {
		t.Error("Should work with empty config")
	}
}
