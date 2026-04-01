package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/provider"
	"github.com/altcode-ai/altcode/internal/store"
	"github.com/altcode-ai/altcode/internal/sysctl"
	"github.com/altcode-ai/altcode/internal/tool"
)

const maxIterations = 50

// EngineParams holds all dependencies for creating an Engine.
type EngineParams struct {
	Config       *config.Config
	Perm         *permission.Evaluator  // nil = allow all
	Store        *store.DB              // nil = no persistence
	SessionID    string                 // empty = new session
	Messages     []provider.Message     // pre-loaded for session resume
	Hooks        *hooks.Runner          // nil = no hooks
	Instructions []config.Instruction   // loaded from CLAUDE.md etc.
}

// Engine orchestrates conversation turns between the user and an AI provider.
type Engine struct {
	cfg          *config.Config
	provider     provider.Provider
	tools        *tool.Registry
	perm         *permission.Evaluator
	hooks        *hooks.Runner
	store        *store.DB
	sessionID    string
	model        string
	messages     []provider.Message
	instructions []config.Instruction
}

// New creates an Engine from the given parameters.
func New(params EngineParams) (*Engine, error) {
	cfg := params.Config
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

	perm := params.Perm
	if perm == nil {
		perm = permission.NewEvaluator(permission.ModeBypass, "", nil)
	}

	msgs := params.Messages
	if msgs == nil {
		msgs = []provider.Message{}
	}

	hooksRunner := params.Hooks
	if hooksRunner == nil {
		hooksRunner = hooks.NewRunner(nil)
	}

	return &Engine{
		cfg:          cfg,
		provider:     p,
		tools:        registry,
		perm:         perm,
		hooks:        hooksRunner,
		store:        params.Store,
		sessionID:    params.SessionID,
		model:        modelName,
		messages:     msgs,
		instructions: params.Instructions,
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

// Messages returns the current conversation history.
func (e *Engine) Messages() []provider.Message {
	return e.messages
}

// SessionID returns the current session ID.
func (e *Engine) SessionID() string {
	return e.sessionID
}

func (e *Engine) persistMessage(role string, msg provider.Message) {
	if e.store == nil || e.sessionID == "" {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	e.store.AddMessage(e.sessionID, role, data, e.model, 0, 0)
}

func (e *Engine) loop(ctx context.Context, input string, out chan<- event.Event) {
	userMsg := provider.TextMessage("user", input)
	e.messages = append(e.messages, userMsg)
	e.persistMessage("user", userMsg)

	defer func() { out <- event.Event{Type: event.Done} }()

	for i := 0; i < maxIterations; i++ {
		if ctx.Err() != nil {
			out <- event.Event{Type: event.ErrorEvent, Error: ctx.Err().Error()}
			return
		}

		stream, err := e.callProvider(ctx)
		if err != nil {
			out <- event.Event{Type: event.ErrorEvent, Error: err.Error()}
			return
		}

		turn := collectTurn(stream, out)

		if len(turn.ToolCalls) == 0 {
			assistMsg := provider.TextMessage("assistant", turn.Text)
			e.messages = append(e.messages, assistMsg)
			e.persistMessage("assistant", assistMsg)

			// Fire Stop hooks — may block completion
			if reason := e.fireStopHooks(ctx); reason != "" {
				e.messages = append(e.messages, provider.TextMessage("user", reason))
				continue // loop back for another turn
			}
			return
		}

		e.appendAssistantWithTools(turn)
		results := e.dispatchTools(ctx, turn.ToolCalls, out)
		e.appendToolResults(turn.ToolCalls, results)
	}
}

func (e *Engine) callProvider(ctx context.Context) (<-chan provider.StreamEvent, error) {
	env := sysctl.DetectEnv()
	system := sysctl.BuildSystemPrompt(e.cfg, e.tools, e.instructions, env)

	req := &provider.Request{
		Model:    e.model,
		Messages: e.messages,
		System:   system,
		Tools:    e.toolSchemas(),
		MaxTokens: 4096,
	}
	return e.provider.Stream(ctx, req)
}

func (e *Engine) appendAssistantWithTools(turn *turnResult) {
	var parts []provider.ContentPart
	if turn.Text != "" {
		parts = append(parts, provider.ContentPart{Type: "text", Text: turn.Text})
	}
	for _, tc := range turn.ToolCalls {
		parts = append(parts, provider.ContentPart{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		})
	}
	e.messages = append(e.messages, provider.Message{Role: "assistant", Parts: parts})
}

func (e *Engine) dispatchTools(
	ctx context.Context,
	toolCalls []collectedToolCall,
	out chan<- event.Event,
) []tool.Result {
	calls := make([]tool.Call, 0, len(toolCalls))
	for _, tc := range toolCalls {
		t, ok := e.tools.Get(tc.Name)
		if !ok {
			calls = append(calls, tool.Call{
				ID: tc.ID, Tool: nil, Input: tc.Input,
				EagerResult: &tool.Result{
					Output: fmt.Sprintf("Error: unknown tool %q", tc.Name),
					Title:  tc.Name,
				},
			})
			continue
		}

		action := e.checkPermission(t, tc)
		switch action {
		case permission.ActionDeny:
			calls = append(calls, tool.Call{
				ID: tc.ID, Tool: t, Input: tc.Input,
				EagerResult: &tool.Result{
					Output: fmt.Sprintf("Permission denied for tool %q", tc.Name),
					Title:  tc.Name,
				},
			})
		case permission.ActionAsk:
			result := e.askPermission(tc, out)
			if result.Error != nil {
				calls = append(calls, tool.Call{
					ID: tc.ID, Tool: t, Input: tc.Input,
					EagerResult: &tool.Result{
						Output: fmt.Sprintf("Permission denied for tool %q", tc.Name),
						Title:  tc.Name,
					},
				})
			} else {
				calls = append(calls, tool.Call{ID: tc.ID, Tool: t, Input: tc.Input})
			}
		default:
			calls = append(calls, tool.Call{ID: tc.ID, Tool: t, Input: tc.Input})
		}

		e.perm.RecordCall(tc.Name, t.PermissionPattern(tc.Input))
	}

	// Fire PreToolUse hooks — may deny individual calls
	for i, tc := range toolCalls {
		hookResults, _ := e.hooks.Fire(ctx, hooks.PreToolUse, hooks.Input{
			Event:    hooks.PreToolUse,
			ToolName: tc.Name,
			ToolInput: tc.Input,
		})
		if hooks.HasDeny(hookResults) {
			msg := "Blocked by hook"
			if msgs := hooks.Messages(hookResults); len(msgs) > 0 {
				msg = msgs[0]
			}
			calls[i].EagerResult = &tool.Result{
				Output: msg,
				Title:  tc.Name,
			}
		}
	}

	results := tool.Dispatch(ctx, calls)

	for i, r := range results {
		out <- event.Event{
			Type:       event.ToolResultEvent,
			ToolResult: &event.Result{Output: r.Output, Title: r.Title},
			ToolCall:   &event.ToolCall{ID: toolCalls[i].ID, Name: toolCalls[i].Name},
		}
	}

	return results
}

func (e *Engine) checkPermission(t tool.Tool, tc collectedToolCall) permission.ActionType {
	pattern := t.PermissionPattern(tc.Input)
	return e.perm.CheckWithReadOnly(tc.Name, pattern, t.IsReadOnly())
}

func (e *Engine) askPermission(tc collectedToolCall, out chan<- event.Event) tool.Result {
	respCh := make(chan event.PermResponse, 1)
	out <- event.Event{
		Type: event.PermissionRequest,
		Permission: &event.PermReq{
			ToolName: tc.Name,
			Pattern:  tc.Name + ":" + string(tc.Input),
			Response: respCh,
		},
	}

	resp, ok := <-respCh
	if !ok || resp.Action != event.Allow {
		return tool.Result{Error: fmt.Errorf("permission denied by user")}
	}
	return tool.Result{}
}

func (e *Engine) appendToolResults(toolCalls []collectedToolCall, results []tool.Result) {
	var parts []provider.ContentPart
	for i, tc := range toolCalls {
		output := results[i].Output
		if results[i].Error != nil {
			output = fmt.Sprintf("Error: %v", results[i].Error)
		}
		parts = append(parts, provider.NewToolResultPart(tc.ID, output))
	}
	e.messages = append(e.messages, provider.ToolResultMessage(parts))
}

func (e *Engine) fireStopHooks(ctx context.Context) string {
	results, _ := e.hooks.Fire(ctx, hooks.Stop, hooks.Input{
		Event:     hooks.Stop,
		SessionID: e.sessionID,
	})
	if hooks.HasDeny(results) {
		if msgs := hooks.Messages(results); len(msgs) > 0 {
			return msgs[0]
		}
		return "Stop hook blocked completion."
	}
	return ""
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
