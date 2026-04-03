package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/provider"
)

// runPromptHook sends the hook prompt to the LLM and parses for allow/deny.
func runPromptHook(
	ctx context.Context,
	p provider.Provider,
	model string,
	entry EntryConfig,
	input Input,
) (*Result, error) {
	if p == nil {
		return nil, fmt.Errorf("no provider for prompt hook")
	}

	prompt := expandTemplate(entry.Prompt, input)
	timeout := resolveTimeout(entry.Timeout)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, err := callProvider(ctx, p, model, prompt)
	if err != nil {
		return nil, fmt.Errorf("prompt hook: %w", err)
	}

	return parseDecision(response), nil
}

// expandTemplate replaces placeholders in the prompt template.
func expandTemplate(tmpl string, input Input) string {
	r := strings.NewReplacer(
		"$TOOL_NAME", input.ToolName,
		"$TOOL_INPUT", string(input.ToolInput),
		"$USER_PROMPT", input.ToolOutput,
	)
	return r.Replace(tmpl)
}

func resolveTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return time.Duration(defaultTimeout) * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// callProvider sends a simple message and collects the text response.
func callProvider(
	ctx context.Context,
	p provider.Provider,
	model, prompt string,
) (string, error) {
	req := &provider.Request{
		Model: model,
		Messages: []provider.Message{
			provider.TextMessage("user", prompt),
		},
		MaxTokens: 256,
	}

	stream, err := p.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	return drainText(stream), nil
}

// drainText reads all text deltas from a stream into a string.
func drainText(stream <-chan provider.StreamEvent) string {
	var b strings.Builder
	for ev := range stream {
		if ev.Type == provider.StreamTextDelta {
			b.WriteString(ev.Delta)
		}
	}
	return b.String()
}

// parseDecision extracts allow/deny from the model's response text.
func parseDecision(response string) *Result {
	lower := strings.ToLower(strings.TrimSpace(response))
	if strings.Contains(lower, "deny") {
		return &Result{Decision: "deny", Message: response}
	}
	return &Result{Decision: "allow", Message: response}
}
