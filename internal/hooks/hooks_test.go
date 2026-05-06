//go:build !windows

package hooks_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/hooks"
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
	// Non-JSON output should NOT default to allow — empty decision
	// lets the next layer (permission evaluator) decide policy.
	if results[0].Decision != "" {
		t.Errorf("Expected empty decision for non-JSON, got %q", results[0].Decision)
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

// =============================================================================
// MULTIPLE HOOKS PER MATCHER
// =============================================================================

func TestMultipleHooksPerMatcher(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"allow","message":"hook1"}'`},
				{Type: "command", Command: `echo '{"decision":"allow","message":"hook2"}'`},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) != 2 {
		t.Fatalf("Expected 2 results from 2 hooks, got %d", len(results))
	}
	if results[0].Message != "hook1" {
		t.Errorf("First hook message: %q", results[0].Message)
	}
	if results[1].Message != "hook2" {
		t.Errorf("Second hook message: %q", results[1].Message)
	}
}

// =============================================================================
// MULTIPLE MATCHERS FOR SAME EVENT
// =============================================================================

func TestMultipleMatchersForEvent(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "Bash", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"allow","message":"bash-hook"}'`},
			}},
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"allow","message":"all-hook"}'`},
			}},
		},
	})

	// Bash should match both matchers
	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// Read should match only wildcard
	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result for Read, got %d", len(results))
	}
	if results[0].Message != "all-hook" {
		t.Errorf("Wrong hook fired for Read: %q", results[0].Message)
	}
}

// =============================================================================
// STOP EVENT HOOKS
// =============================================================================

func TestStopHook_Allow(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"allow"}'`},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.Stop, hooks.Input{
		Event: hooks.Stop, SessionID: "sess-1",
	})
	if hooks.HasDeny(results) {
		t.Error("Stop hook should allow")
	}
}

func TestStopHook_Deny(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"deny","message":"not finished"}'`},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.Stop, hooks.Input{
		Event: hooks.Stop, SessionID: "sess-1",
	})
	if !hooks.HasDeny(results) {
		t.Error("Stop hook should deny")
	}
	msgs := hooks.Messages(results)
	if len(msgs) == 0 || msgs[0] != "not finished" {
		t.Errorf("Expected deny message, got %v", msgs)
	}
}

// =============================================================================
// POSTTOOLUSE EVENT
// =============================================================================

func TestPostToolUseHook(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PostToolUse: {
			{Matcher: "Write", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"allow","message":"file written"}'`},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PostToolUse, hooks.Input{
		Event:      hooks.PostToolUse,
		ToolName:   "Write",
		ToolInput:  json.RawMessage(`{"file_path":"/tmp/x"}`),
		ToolOutput: "Created /tmp/x",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Message != "file written" {
		t.Errorf("Message: %q", results[0].Message)
	}
}

// =============================================================================
// SESSIONSTART EVENT
// =============================================================================

func TestSessionStartHook(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.SessionStart: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: `echo '{"decision":"allow","message":"session started"}'`},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.SessionStart, hooks.Input{
		Event:     hooks.SessionStart,
		SessionID: "test-session",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Message != "session started" {
		t.Errorf("Message: %q", results[0].Message)
	}
}

// =============================================================================
// HOOK ERROR HANDLING
// =============================================================================

func TestHookCommand_ErrorDefaultsToAllow(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: "exit 1"}, // non-zero, non-2 exit
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	// Non-zero non-2 exit should result in error, which is treated as allow
	if results[0].Decision == "deny" {
		t.Error("Non-deny exit code should not deny")
	}
}

func TestHookCommand_UnknownTypeDefaultsToAllow(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "*", Hooks: []hooks.EntryConfig{
				{Type: "unknown_type"},
			}},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Decision != "allow" {
		t.Errorf("Unknown type should default to allow, got %q", results[0].Decision)
	}
}

// =============================================================================
// CASE INSENSITIVE MATCHING
// =============================================================================

func TestMatchTool_CaseInsensitive(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{Matcher: "Write|Edit", Hooks: []hooks.EntryConfig{
				{Type: "command", Command: "echo ok"},
			}},
		},
	})

	// lowercase tool name should match PascalCase matcher
	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "write",
	})
	if len(results) == 0 {
		t.Error("Should match write (case-insensitive)")
	}

	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "EDIT",
	})
	if len(results) == 0 {
		t.Error("Should match EDIT (case-insensitive)")
	}
}

// =============================================================================
// HASDENY AND MESSAGES EDGE CASES
// =============================================================================

func TestHasDeny_EmptyResults(t *testing.T) {
	if hooks.HasDeny([]hooks.Result{}) {
		t.Error("Empty results should not have deny")
	}
}

func TestMessages_Empty(t *testing.T) {
	msgs := hooks.Messages(nil)
	if len(msgs) != 0 {
		t.Error("nil results should return empty messages")
	}
	msgs = hooks.Messages([]hooks.Result{})
	if len(msgs) != 0 {
		t.Error("Empty results should return empty messages")
	}
}
