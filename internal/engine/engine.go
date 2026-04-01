package engine

import (
	"context"
	"fmt"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/tool"
)

// Engine orchestrates conversation turns between the user and an AI provider.
type Engine struct {
	cfg      *config.Config
	provider provider.Provider
	tools    *tool.Registry
	model    string
	messages []provider.Message
}

// New creates an Engine from the given configuration.
func New(cfg *config.Config) (*Engine, error) {
	providerName, modelName := parseModel(cfg.Model)

	var p provider.Provider
	switch providerName {
	case "anthropic":
		pcfg := cfg.Provider["anthropic"]
		p = provider.NewAnthropic(provider.AnthropicConfig{
			APIKey:  pcfg.APIKey,
			BaseURL: pcfg.BaseURL,
		})
	default:
		return nil, fmt.Errorf("unsupported provider: %s", providerName)
	}

	registry := tool.NewRegistry()
	registry.Register(tool.NewReadTool())
	registry.Register(tool.NewGlobTool())
	registry.Register(tool.NewGrepTool())
	registry.Register(tool.NewLsTool())
	registry.Register(tool.NewBashTool())
	registry.Register(tool.NewEditTool())
	registry.Register(tool.NewWriteTool())

	return &Engine{
		cfg:      cfg,
		provider: p,
		tools:    registry,
		model:    modelName,
	}, nil
}

// Run sends user input to the provider and returns a channel of events.
func (e *Engine) Run(ctx context.Context, input string) <-chan event.Event {
	events := make(chan event.Event, 64)
	go func() {
		defer close(events)
		e.loop(ctx, input, events)
	}()
	return events
}

func (e *Engine) loop(ctx context.Context, input string, out chan<- event.Event) {
	e.messages = append(e.messages, provider.Message{
		Role: "user", Content: input,
	})

	req := &provider.Request{
		Model:    e.model,
		Messages: e.messages,
		System: []provider.SystemSection{
			{Content: "You are a helpful coding assistant. Be concise."},
		},
		Tools:     e.toolSchemas(),
		MaxTokens: 4096,
	}

	stream, err := e.provider.Stream(ctx, req)
	if err != nil {
		out <- event.Event{Type: event.ErrorEvent, Error: err.Error()}
		return
	}

	var fullText string
	for sev := range stream {
		switch sev.Type {
		case provider.StreamTextDelta:
			fullText += sev.Delta
			out <- event.Event{Type: event.TextDelta, Text: sev.Delta}
		case provider.StreamThinkingDelta:
			out <- event.Event{Type: event.ThinkingDelta, Thinking: sev.Delta}
		case provider.StreamUsage:
			if sev.Usage != nil {
				out <- event.Event{Type: event.UsageEvent, Usage: &event.UsageInfo{
					InputTokens:  sev.Usage.InputTokens,
					OutputTokens: sev.Usage.OutputTokens,
				}}
			}
		case provider.StreamError:
			out <- event.Event{Type: event.ErrorEvent, Error: sev.Error.Error()}
			return
		case provider.StreamDone:
			out <- event.Event{Type: event.TextDone, Text: fullText}
		}
	}

	e.messages = append(e.messages, provider.Message{
		Role: "assistant", Content: fullText,
	})
	out <- event.Event{Type: event.Done}
}

func (e *Engine) toolSchemas() []provider.ToolSchema {
	var schemas []provider.ToolSchema
	for _, t := range e.tools.All() {
		schemas = append(schemas, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Parameters(),
		})
	}
	return schemas
}

func parseModel(model string) (providerName, modelName string) {
	for i, c := range model {
		if c == '/' {
			return model[:i], model[i+1:]
		}
	}
	return "anthropic", model
}
