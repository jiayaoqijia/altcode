package hooks_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/altcode-ai/altcode/internal/hooks"
)

func TestMatchTool_Exact(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "Bash", Hooks: []hooks.EntryConfig{{Type: "command", Command: "echo ok"}}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) == 0 {
		t.Error("Expected match for exact tool name")
	}

	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) != 0 {
		t.Error("Should not match different tool")
	}
}

func TestMatchTool_PipeSeparated(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "Write|Edit", Hooks: []hooks.EntryConfig{{Type: "command", Command: "echo ok"}}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Write",
	})
	if len(results) == 0 {
		t.Error("Should match Write")
	}

	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Edit",
	})
	if len(results) == 0 {
		t.Error("Should match Edit")
	}

	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) != 0 {
		t.Error("Should not match Read")
	}
}

func TestMatchTool_Wildcard(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{{Type: "command", Command: "echo ok"}}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "anything",
	})
	if len(results) == 0 {
		t.Error("Wildcard should match any tool")
	}
}

func TestHookCommand_Success(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"allow","message":"ok"}'`},
			}},
		},
	})

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
	if results[0].Message != "ok" {
		t.Errorf("Expected message 'ok', got %q", results[0].Message)
	}
}

func TestHookCommand_Deny(t *testing.T) {
	// Write a script that exits with code 2 (deny)
	dir := t.TempDir()
	script := filepath.Join(dir, "deny.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'Blocked!' >&2\nexit 2\n"), 0o755)

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "Bash", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: "sh " + script},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
		ToolInput: json.RawMessage(`{"command":"rm -rf /"}`),
	})
	if len(results) == 0 {
		t.Fatal("Expected result")
	}
	if results[0].Decision != "deny" {
		t.Errorf("Expected deny, got %q", results[0].Decision)
	}
	if results[0].Message == "" {
		t.Error("Expected deny message from stderr")
	}
}

func TestHookCommand_StdinReceivesInput(t *testing.T) {
	// Script that reads stdin and echoes the toolName
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps({'decision':'allow','message':d['toolName']}))"`},
			}},
		},
	})

	results, err := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "read",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Message != "read" {
		t.Errorf("Expected toolName in message, got %q", results[0].Message)
	}
}

func TestHookCommand_EmptyOutput(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: "true"},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Decision != "allow" {
		t.Errorf("Empty output should default to allow, got %q", results[0].Decision)
	}
}

func TestHookCommand_NonJSONOutput(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: "echo 'not json'"},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	// Non-JSON output should be treated as allow with message
	if results[0].Decision != "allow" {
		t.Errorf("Expected allow for non-JSON, got %q", results[0].Decision)
	}
}

func TestNoHooksConfigured(t *testing.T) {
	r := hooks.NewRunner(nil)
	results, err := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Error("Expected no results for no hooks")
	}
}

func TestNoMatchingEvent(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{{Type: "command", Command: "echo stop"}}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) != 0 {
		t.Error("Should have no results for non-matching event")
	}
}

func TestHasDeny(t *testing.T) {
	if hooks.HasDeny(nil) {
		t.Error("nil should not have deny")
	}
	if hooks.HasDeny([]hooks.Result{{Decision: "allow"}}) {
		t.Error("allow should not be deny")
	}
	if !hooks.HasDeny([]hooks.Result{{Decision: "deny"}}) {
		t.Error("deny should be deny")
	}
	if !hooks.HasDeny([]hooks.Result{{Decision: "allow"}, {Decision: "deny"}}) {
		t.Error("mixed with deny should be deny")
	}
}

func TestMessages(t *testing.T) {
	msgs := hooks.Messages([]hooks.Result{
		{Message: "first"},
		{Message: ""},
		{Message: "third"},
	})
	if len(msgs) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(msgs))
	}
}
