package hooks

import (
	"context"
	"encoding/json"

	"github.com/altcode-ai/altcode/internal/provider"
)

// Event identifies when a hook fires.
type Event string

const (
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	Stop             Event = "Stop"
	SessionStart     Event = "SessionStart"
	SessionEnd       Event = "SessionEnd"
	UserPromptSubmit Event = "UserPromptSubmit"
	SubagentStop     Event = "SubagentStop"
	PreCompact       Event = "PreCompact"
	Notification     Event = "Notification"
	CwdChanged       Event = "CwdChanged"
	FileChanged      Event = "FileChanged"
	TaskCreated      Event = "TaskCreated"
	PermissionDenied Event = "PermissionDenied"
)

// MatcherConfig pairs a tool name matcher with hook entries.
type MatcherConfig struct {
	Matcher string        `json:"matcher"`
	If      string        `json:"if,omitempty"` // e.g. "Bash(git *)"
	Hooks   []EntryConfig `json:"hooks"`
}

// EntryConfig defines a single hook action.
type EntryConfig struct {
	Type    string `json:"type"`              // "command", "prompt", or "http"
	Command string `json:"command,omitempty"` // shell command (type=command)
	Prompt  string `json:"prompt,omitempty"`  // LLM prompt template (type=prompt)
	URL     string `json:"url,omitempty"`     // webhook URL (type=http)
	Timeout int    `json:"timeout,omitempty"` // seconds (default 30)
}

// Input is the JSON payload sent to hook commands on stdin.
type Input struct {
	Event      Event           `json:"event"`
	ToolName   string          `json:"toolName,omitempty"`
	ToolInput  json.RawMessage `json:"toolInput,omitempty"`
	ToolOutput string          `json:"toolOutput,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	// UserPrompt is the prompt the user typed (UserPromptSubmit only).
	// Without this field, prompt hooks expanding $USER_PROMPT and
	// command hooks reading the JSON payload both saw an empty value.
	UserPrompt string `json:"userPrompt,omitempty"`
}

// Result is the JSON payload returned by a hook command.
type Result struct {
	Decision     string          `json:"decision,omitempty"`     // "allow", "deny"
	Message      string          `json:"message,omitempty"`      // fed to agent
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"` // modified input
}

// Runner manages hook configurations and executes matching hooks.
type Runner struct {
	configs  map[Event][]MatcherConfig
	provider provider.Provider // used by prompt hooks
	model    string            // model for prompt hooks
}

// NewRunner creates a Runner from the given hook configurations.
func NewRunner(configs map[Event][]MatcherConfig) *Runner {
	if configs == nil {
		configs = make(map[Event][]MatcherConfig)
	}
	return &Runner{configs: configs}
}

// SetProvider configures the LLM provider for prompt-type hooks.
func (r *Runner) SetProvider(p provider.Provider, model string) {
	r.provider = p
	r.model = model
}

// Fire executes all hooks matching the event and tool name.
// Returns results from all hooks. Hooks run sequentially within a matcher.
func (r *Runner) Fire(ctx context.Context, ev Event, input Input) ([]Result, error) {
	matchers, ok := r.configs[ev]
	if !ok {
		return nil, nil
	}

	var results []Result
	for _, mc := range matchers {
		if !matchTool(mc.Matcher, input.ToolName) {
			continue
		}
		if mc.If != "" && !matchCondition(mc.If, input) {
			continue
		}
		for _, entry := range mc.Hooks {
			result, err := r.executeHookEntry(ctx, entry, input)
			if err != nil {
				results = append(results, Result{
					Decision: "allow",
					Message:  "Hook error: " + err.Error(),
				})
				continue
			}
			results = append(results, *result)
		}
	}
	return results, nil
}

// executeHookEntry dispatches to the correct hook type.
func (r *Runner) executeHookEntry(
	ctx context.Context,
	entry EntryConfig,
	input Input,
) (*Result, error) {
	switch entry.Type {
	case "command":
		return runCommandHook(ctx, entry, input)
	case "prompt":
		return runPromptHook(ctx, r.provider, r.model, entry, input)
	case "http":
		return runHTTPHook(ctx, entry, input)
	default:
		return &Result{Decision: "allow"}, nil
	}
}

// HasDeny returns true if any result has decision "deny".
func HasDeny(results []Result) bool {
	for _, r := range results {
		if r.Decision == "deny" {
			return true
		}
	}
	return false
}

// Messages returns all non-empty messages from results.
func Messages(results []Result) []string {
	var msgs []string
	for _, r := range results {
		if r.Message != "" {
			msgs = append(msgs, r.Message)
		}
	}
	return msgs
}
