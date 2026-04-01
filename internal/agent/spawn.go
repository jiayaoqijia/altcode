package agent

import (
	"context"
	"strings"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

// Spawn creates a child engine with the agent's restrictions and runs it.
// The child has its own message history, restricted tools, and optional
// model override. It returns an event channel just like Engine.Run.
func Spawn(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	input string,
) <-chan event.Event {
	childCfg := *parent.Config()
	resolveModel(&childCfg, ag.Model)

	instructions := []config.Instruction{{
		Path:    ag.Path,
		Content: ag.SystemPrompt,
	}}

	tools := parent.Registry()
	if len(ag.Tools) > 0 {
		tools = tools.Subset(ag.Tools)
	}

	params := engine.EngineParams{
		Config:       &childCfg,
		Instructions: instructions,
	}

	child, err := engine.NewWithRegistry(params, tools)
	if err != nil {
		ch := make(chan event.Event, 1)
		ch <- event.Event{Type: event.ErrorEvent, Error: err.Error()}
		close(ch)
		return ch
	}

	return child.Run(ctx, input)
}

func resolveModel(cfg *config.Config, model string) {
	switch model {
	case "", "inherit":
		// keep parent model
	case "sonnet", "opus", "haiku":
		// Claude Code agents use short names like "sonnet", "opus", "haiku".
		// When running with a non-Anthropic provider, keep the parent model
		// to avoid provider mismatch. Only override if already on Anthropic.
		if strings.HasPrefix(cfg.Model, "anthropic/") || !strings.Contains(cfg.Model, "/") {
			// Anthropic provider — resolve short name
			switch model {
			case "sonnet":
				cfg.Model = "anthropic/claude-sonnet-4-20250514"
			case "opus":
				cfg.Model = "anthropic/claude-opus-4-6-20250514"
			case "haiku":
				cfg.Model = "anthropic/claude-haiku-4-5-20251001"
			}
		}
		// Non-Anthropic (OpenAI/Ollama) — keep parent model
	default:
		cfg.Model = model
	}
}

