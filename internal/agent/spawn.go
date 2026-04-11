package agent

import (
	"context"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/provider"
)

// ForkMode controls how much parent history a subagent inherits.
type ForkMode int

const (
	// ForkFresh creates a clean child with no parent history (default).
	ForkFresh ForkMode = iota
	// ForkFullHistory gives the child the complete parent conversation.
	ForkFullHistory
	// ForkLastNTurns gives the child the last N user turns from the parent.
	ForkLastNTurns
)

// SpawnOptions configures how a subagent is created.
type SpawnOptions struct {
	ForkMode  ForkMode
	ForkTurns int           // for ForkLastNTurns — how many recent turns to include
	Mailbox   *Mailbox      // optional shared mailbox for inter-agent comms
	Timeout   time.Duration // 0 = no timeout; agent is cancelled after this duration
}

// Spawn creates a child engine with the agent's restrictions and runs it.
// Supports history forking so subagents can inherit parent context.
func Spawn(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	input string,
) <-chan event.Event {
	return SpawnWithOptions(ctx, parent, ag, input, SpawnOptions{})
}

// SpawnWithOptions creates a child engine with configurable fork mode.
func SpawnWithOptions(
	ctx context.Context,
	parent *engine.Engine,
	ag *Agent,
	input string,
	opts SpawnOptions,
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

	// History forking: inherit parent messages based on fork mode
	var messages []provider.Message
	switch opts.ForkMode {
	case ForkFullHistory:
		src := parent.Messages()
		messages = make([]provider.Message, len(src))
		copy(messages, src)
		// Inject forked-context message
		messages = append(messages, provider.TextMessage("user",
			"You are a newly spawned agent. The prior conversation was forked from your parent. Treat the next message as your new task."))
	case ForkLastNTurns:
		// A negative or zero ForkTurns previously caused make(slice, neg)
		// to panic ("makeslice: len out of range") on the engine
		// goroutine, killing the whole session. Treat <=0 as "no fork".
		if opts.ForkTurns <= 0 {
			break
		}
		src := parent.Messages()
		n := opts.ForkTurns * 2 // approximate: each turn = user + assistant
		if n > len(src) {
			n = len(src)
		}
		messages = make([]provider.Message, n)
		copy(messages, src[len(src)-n:])
	default:
		// ForkFresh — no history
	}

	params := engine.EngineParams{
		Config:       &childCfg,
		Instructions: instructions,
		Messages:     messages,
		TokenBudget:  parent.TokenBudget(),
	}

	child, err := engine.NewWithRegistry(params, tools)
	if err != nil {
		ch := make(chan event.Event, 1)
		ch <- event.Event{Type: event.ErrorEvent, Error: err.Error()}
		close(ch)
		return ch
	}

	// Apply timeout if configured. The previous version discarded the
	// CancelFunc, which leaks the timer's resources until the deadline
	// fires — even when the child completes normally well before then.
	// Wrap child.Run so cancel() runs after the event channel closes.
	runCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		raw := child.Run(runCtx, input)
		out := make(chan event.Event, cap(raw))
		go func() {
			defer cancel()
			defer close(out)
			for ev := range raw {
				out <- ev
			}
		}()
		return out
	}

	return child.Run(runCtx, input)
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

