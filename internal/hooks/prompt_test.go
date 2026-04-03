//go:build !windows

package hooks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/provider"
)

func sseResponse(text string) string {
	return `event: content_block_start
data: {"index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"delta":{"type":"text_delta","text":"` + text + `"}}

event: content_block_stop
data: {}

event: message_stop
data: {}

`
}

func makeProvider(t *testing.T, response string) (provider.Provider, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseResponse(response)))
	}))
	t.Cleanup(srv.Close)

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey: "test-key", BaseURL: srv.URL,
	})
	return p, "test-model"
}

func TestPromptHook_Allow(t *testing.T) {
	p, model := makeProvider(t, "allow")

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks: []hooks.EntryConfig{{
				Type:   "prompt",
				Prompt: "Should $TOOL_NAME be allowed?",
			}},
		}},
	})
	r.SetProvider(p, model)

	results, err := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Decision != "allow" {
		t.Errorf("Expected allow, got %q", results[0].Decision)
	}
}

func TestPromptHook_Deny(t *testing.T) {
	p, model := makeProvider(t, "deny this action")

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "Bash",
			Hooks: []hooks.EntryConfig{{
				Type:   "prompt",
				Prompt: "Is $TOOL_NAME safe?",
			}},
		}},
	})
	r.SetProvider(p, model)

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Decision != "deny" {
		t.Errorf("Expected deny, got %q", results[0].Decision)
	}
}

func TestPromptHook_NoProvider(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks: []hooks.EntryConfig{{
				Type:   "prompt",
				Prompt: "Check this",
			}},
		}},
	})
	// No SetProvider called

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Message, "no provider") {
		t.Errorf("Expected provider error, got %q", results[0].Message)
	}
}

func TestPromptHook_TemplateExpansion(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf [8192]byte
		n, _ := r.Body.Read(buf[:])
		capturedBody = string(buf[:n])
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(sseResponse("allow")))
	}))
	t.Cleanup(srv.Close)

	p := provider.NewAnthropic(provider.AnthropicConfig{
		APIKey: "k", BaseURL: srv.URL,
	})

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks: []hooks.EntryConfig{{
				Type:   "prompt",
				Prompt: "Tool=$TOOL_NAME Input=$TOOL_INPUT",
			}},
		}},
	})
	r.SetProvider(p, "test-model")

	r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Write",
		ToolInput: json.RawMessage(`{"path":"/tmp/test"}`),
	})

	if !strings.Contains(capturedBody, "Tool=Write") {
		t.Error("Expected $TOOL_NAME expansion")
	}
	if !strings.Contains(capturedBody, "/tmp/test") {
		t.Error("Expected $TOOL_INPUT expansion")
	}
}

func TestIfCondition_Matches(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "Bash",
			If:      "Bash(git)",
			Hooks: []hooks.EntryConfig{{
				Type: "command", Command: `echo '{"decision":"deny"}'`,
			}},
		}},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`"git push"`),
	})
	if len(results) == 0 {
		t.Error("Expected match on Bash(git) condition")
	}
}

func TestIfCondition_NoMatch(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "Bash",
			If:      "Bash(git)",
			Hooks: []hooks.EntryConfig{{
				Type: "command", Command: `echo '{"decision":"deny"}'`,
			}},
		}},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`"ls -la"`),
	})
	if len(results) != 0 {
		t.Error("Should not match: input does not contain 'git'")
	}
}

func TestIfCondition_WrongTool(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			If:      "Bash(git)",
			Hooks: []hooks.EntryConfig{{
				Type: "command", Command: `echo '{"decision":"deny"}'`,
			}},
		}},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:    hooks.PreToolUse,
		ToolName: "Read",
	})
	if len(results) != 0 {
		t.Error("Should not match: tool is Read, not Bash")
	}
}

func TestIfCondition_Empty(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			If:      "",
			Hooks: []hooks.EntryConfig{{
				Type: "command", Command: "echo ok",
			}},
		}},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) == 0 {
		t.Error("Empty If should match all")
	}
}
