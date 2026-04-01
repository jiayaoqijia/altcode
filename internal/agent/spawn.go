package agent

import (
	"context"

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
	default:
		cfg.Model = model
	}
}

