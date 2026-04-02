//go:build !windows

package internal_test

// Tests ported from Claude Code's hook system patterns.
// Covers: validate-bash, validate-write, stop-hook state,
// session-start injection, rule matching, YAML frontmatter,
// PostToolUse flow, hook timeout, multiple hooks deny-wins,
// and exit code protocol.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/command"
	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/hooks"
)

// =====================================================================
// 1. validate-bash.sh pattern: safe vs dangerous bash commands
// =====================================================================

func TestClaudeCode_ValidateBashHook(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "validate-bash.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
INPUT=$(cat)
CMD=$(echo "$INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('toolInput',{}).get('command',''))" 2>/dev/null)

case "$CMD" in
  ls*|pwd|echo*)
    echo '{"decision":"allow","message":"safe command"}'
    ;;
  *rm\ -rf*)
    echo "BLOCKED: dangerous rm -rf" >&2
    exit 2
    ;;
  *)
    echo '{"decision":"allow"}'
    ;;
esac
`), 0o755)

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "Bash",
			Hooks: []hooks.EntryConfig{
				{Type: "command", Command: "sh " + script},
			},
		}},
	})

	// Safe command
	results, err := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`{"command":"ls -la"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if hooks.HasDeny(results) {
		t.Error("ls should be allowed")
	}

	// Dangerous command
	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`{"command":"rm -rf /"}`),
	})
	if !hooks.HasDeny(results) {
		t.Error("rm -rf should be denied")
	}
	msgs := hooks.Messages(results)
	found := false
	for _, m := range msgs {
		if strings.Contains(m, "BLOCKED") {
			found = true
		}
	}
	if !found {
		t.Errorf("Deny message missing: %v", msgs)
	}
}

// =====================================================================
// 2. validate-write.sh pattern: path traversal and system dirs
// =====================================================================

func TestClaudeCode_ValidateWriteHook(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "validate-write.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
INPUT=$(cat)
PATH_VAL=$(echo "$INPUT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('toolInput',{}).get('file_path',''))" 2>/dev/null)

case "$PATH_VAL" in
  *..*)
    echo "BLOCKED: path traversal" >&2
    exit 2
    ;;
  /etc/*|/sys/*)
    echo "BLOCKED: system directory" >&2
    exit 2
    ;;
  *)
    echo '{"decision":"allow"}'
    ;;
esac
`), 0o755)

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "Write|Edit",
			Hooks: []hooks.EntryConfig{
				{Type: "command", Command: "sh " + script},
			},
		}},
	})

	// Normal path
	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Write",
		ToolInput: json.RawMessage(`{"file_path":"/home/user/code/main.go"}`),
	})
	if hooks.HasDeny(results) {
		t.Error("Normal path should be allowed")
	}

	// Path traversal
	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"/home/user/../../etc/passwd"}`),
	})
	if !hooks.HasDeny(results) {
		t.Error("Path traversal should be denied")
	}

	// System directory
	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:     hooks.PreToolUse,
		ToolName:  "Write",
		ToolInput: json.RawMessage(`{"file_path":"/etc/shadow"}`),
	})
	if !hooks.HasDeny(results) {
		t.Error("System dir should be denied")
	}
}

// =====================================================================
// 3. stop-hook.sh (ralph-wiggum): state file + iteration counter
// =====================================================================

func TestClaudeCode_StopHookStateFile(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	os.WriteFile(stateFile, []byte(`{"iteration":0,"max":3}`), 0o644)

	script := filepath.Join(dir, "stop-hook.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
STATE=%s
ITER=$(python3 -c "import json; d=json.load(open('$STATE')); print(d['iteration'])" 2>/dev/null || echo 0)
MAX=$(python3 -c "import json; d=json.load(open('$STATE')); print(d['max'])" 2>/dev/null || echo 3)
NEXT=$((ITER + 1))
python3 -c "import json; json.dump({'iteration':$NEXT,'max':$MAX}, open('$STATE','w'))"

if [ "$NEXT" -lt "$MAX" ]; then
  echo '{"decision":"deny","message":"keep going"}'
else
  echo '{"decision":"allow","message":"done"}'
fi
`, stateFile)), 0o755)

	// Fix: the script uses $STATE but we need the literal path
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
STATE="%s"
ITER=$(python3 -c "import json; d=json.load(open('$STATE')); print(d['iteration'])" 2>/dev/null || echo 0)
MAX=$(python3 -c "import json; d=json.load(open('$STATE')); print(d['max'])" 2>/dev/null || echo 3)
NEXT=$((ITER + 1))
python3 -c "import json; json.dump({'iteration':$NEXT,'max':$MAX}, open('$STATE','w'))"

if [ "$NEXT" -lt "$MAX" ]; then
  echo '{"decision":"deny","message":"keep going"}'
else
  echo '{"decision":"allow","message":"done"}'
fi
`, stateFile)), 0o755)

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.Stop: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	// Invocations 1 and 2 should deny (iteration < max)
	for i := 0; i < 2; i++ {
		results, err := r.Fire(context.Background(), hooks.Stop, hooks.Input{
			Event: hooks.Stop,
		})
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if !hooks.HasDeny(results) {
			t.Errorf("iter %d: should deny (keep going)", i)
		}
	}

	// Invocation 3 should allow (iteration == max)
	results, _ := r.Fire(context.Background(), hooks.Stop, hooks.Input{
		Event: hooks.Stop,
	})
	if hooks.HasDeny(results) {
		t.Error("Third invocation should allow (done)")
	}
}

// =====================================================================
// 4. session-start.sh: inject systemMessage content
// =====================================================================

func TestClaudeCode_SessionStartInjectsContext(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "session-start.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
echo '{"decision":"allow","message":"Always respond in haiku form."}'
`), 0o755)

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.SessionStart: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	results, err := r.Fire(context.Background(), hooks.SessionStart, hooks.Input{
		Event:     hooks.SessionStart,
		SessionID: "sess-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("Expected result from session start hook")
	}

	msgs := hooks.Messages(results)
	if len(msgs) == 0 {
		t.Fatal("Expected message from session start hook")
	}
	if !strings.Contains(msgs[0], "haiku") {
		t.Errorf("Expected haiku instruction, got: %q", msgs[0])
	}
}

// =====================================================================
// 5. hookify rule_engine.py: matcher patterns (exact, pipe, wildcard)
// =====================================================================

func TestClaudeCode_RuleMatcherPatterns(t *testing.T) {
	makeRunner := func(matcher string) *hooks.Runner {
		return hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
			hooks.PreToolUse: {{
				Matcher: matcher,
				Hooks: []hooks.EntryConfig{
					{Type: "command", Command: `echo '{"decision":"allow","message":"matched"}'`},
				},
			}},
		})
	}

	// Exact match
	r := makeRunner("Bash")
	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) == 0 {
		t.Error("Exact: should match Bash")
	}
	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Read",
	})
	if len(results) != 0 {
		t.Error("Exact: should not match Read")
	}

	// Pipe-separated
	r = makeRunner("Read|Write|Edit")
	for _, name := range []string{"Read", "Write", "Edit"} {
		results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
			Event: hooks.PreToolUse, ToolName: name,
		})
		if len(results) == 0 {
			t.Errorf("Pipe: should match %s", name)
		}
	}
	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) != 0 {
		t.Error("Pipe: should not match Bash")
	}

	// Wildcard
	r = makeRunner("*")
	for _, name := range []string{"Bash", "Read", "anything"} {
		results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
			Event: hooks.PreToolUse, ToolName: name,
		})
		if len(results) == 0 {
			t.Errorf("Wildcard: should match %s", name)
		}
	}

	// Empty matcher (acts as wildcard)
	r = makeRunner("")
	results, _ = r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event: hooks.PreToolUse, ToolName: "Bash",
	})
	if len(results) == 0 {
		t.Error("Empty matcher should match everything")
	}
}

// =====================================================================
// 6. hookify config_loader.py: .md files with YAML frontmatter
// =====================================================================

func TestClaudeCode_CommandParserYAMLFrontmatter(t *testing.T) {
	dir := t.TempDir()

	// Full frontmatter with all fields
	os.WriteFile(filepath.Join(dir, "validate.md"), []byte(`---
description: Validate code before commit
argument-hint: file or directory path
allowed-tools: Bash(git diff *), Read
---

Check $ARGUMENTS for issues before committing.
`), 0o644)

	cmd, err := command.ParseFile(filepath.Join(dir, "validate.md"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Description != "Validate code before commit" {
		t.Errorf("Description: %q", cmd.Description)
	}
	if cmd.ArgumentHint != "file or directory path" {
		t.Errorf("ArgumentHint: %q", cmd.ArgumentHint)
	}
	if len(cmd.AllowedTools) != 2 {
		t.Errorf("AllowedTools: %v", cmd.AllowedTools)
	}

	expanded, _ := cmd.Expand("src/main.go")
	if !strings.Contains(expanded, "Check src/main.go") {
		t.Errorf("Expansion: %q", expanded)
	}

	// No frontmatter
	os.WriteFile(filepath.Join(dir, "simple.md"), []byte(
		"Just do the thing.\n",
	), 0o644)

	cmd2, err := command.ParseFile(filepath.Join(dir, "simple.md"))
	if err != nil {
		t.Fatal(err)
	}
	if cmd2.Description != "" {
		t.Error("No frontmatter should have empty description")
	}
	if !strings.Contains(cmd2.Body, "Just do the thing") {
		t.Errorf("Body: %q", cmd2.Body)
	}

	// Empty frontmatter
	os.WriteFile(filepath.Join(dir, "empty-fm.md"), []byte(`---
---

Body only.
`), 0o644)

	cmd3, _ := command.ParseFile(filepath.Join(dir, "empty-fm.md"))
	if !strings.Contains(cmd3.Body, "Body only") {
		t.Errorf("Empty FM body: %q", cmd3.Body)
	}
}

// =====================================================================
// 7. PostToolUse hook: fires after tool execution with tool output
// =====================================================================

func TestClaudeCode_PostToolUseHookFlow(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "post.log")
	script := filepath.Join(dir, "post-hook.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
INPUT=$(cat)
echo "$INPUT" >> %s
echo '{"decision":"allow","message":"post-hook ran"}'
`, logFile)), 0o755)

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PostToolUse: {{
			Matcher: "Bash",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	results, err := r.Fire(context.Background(), hooks.PostToolUse, hooks.Input{
		Event:      hooks.PostToolUse,
		ToolName:   "Bash",
		ToolInput:  json.RawMessage(`{"command":"echo hello"}`),
		ToolOutput: "hello\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("Expected post-hook result")
	}
	if results[0].Message != "post-hook ran" {
		t.Errorf("Message: %q", results[0].Message)
	}

	// Verify the hook received the tool output in its stdin
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("Hook should receive toolOutput: %s", data)
	}
}

// =====================================================================
// 8. Hook timeout: slow hooks don't hang the engine
// =====================================================================

func TestClaudeCode_HookTimeout(t *testing.T) {
	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks: []hooks.EntryConfig{{
				Type:    "command",
				Command: "sleep 30",
				Timeout: 1, // 1 second timeout
			}},
		}},
	})

	start := time.Now()
	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:    hooks.PreToolUse,
		ToolName: "Bash",
	})
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("Hook should timeout quickly, took %v", elapsed)
	}

	// Timed-out hook should default to allow (error path)
	if hooks.HasDeny(results) {
		t.Error("Timed-out hook should not deny")
	}
}

// =====================================================================
// 9. Multiple hooks on same event: deny wins over allow
// =====================================================================

func TestClaudeCode_MultipleHooksDenyWins(t *testing.T) {
	dir := t.TempDir()
	denyScript := filepath.Join(dir, "deny.sh")
	os.WriteFile(denyScript, []byte("#!/bin/sh\necho 'DENIED' >&2\nexit 2\n"), 0o755)

	r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {
			{
				Matcher: "*",
				Hooks: []hooks.EntryConfig{
					{Type: "command", Command: `echo '{"decision":"allow","message":"hook1 ok"}'`},
				},
			},
			{
				Matcher: "*",
				Hooks: []hooks.EntryConfig{
					{Type: "command", Command: "sh " + denyScript},
				},
			},
		},
	})

	results, _ := r.Fire(context.Background(), hooks.PreToolUse, hooks.Input{
		Event:    hooks.PreToolUse,
		ToolName: "Bash",
	})

	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// HasDeny should return true even though one hook allowed
	if !hooks.HasDeny(results) {
		t.Error("Deny should win when any hook denies")
	}

	// Verify both hooks fired
	msgs := hooks.Messages(results)
	if len(msgs) < 2 {
		t.Errorf("Expected messages from both hooks: %v", msgs)
	}
}

// =====================================================================
// 10. Hook exit code protocol: 0=allow, 2=deny+stderr, 1=error+allow
// =====================================================================

func TestClaudeCode_HookExitCodeProtocol(t *testing.T) {
	dir := t.TempDir()

	// Exit 0: allow with JSON output
	exit0 := filepath.Join(dir, "exit0.sh")
	os.WriteFile(exit0, []byte(`#!/bin/sh
echo '{"decision":"allow","message":"permitted"}'
exit 0
`), 0o755)

	// Exit 2: deny with stderr message
	exit2 := filepath.Join(dir, "exit2.sh")
	os.WriteFile(exit2, []byte(`#!/bin/sh
echo "Operation blocked by policy" >&2
exit 2
`), 0o755)

	// Exit 1: error, should default to allow
	exit1 := filepath.Join(dir, "exit1.sh")
	os.WriteFile(exit1, []byte(`#!/bin/sh
echo "something went wrong" >&2
exit 1
`), 0o755)

	tests := []struct {
		name     string
		script   string
		wantDeny bool
		wantMsg  string
	}{
		{
			name:     "exit_0_allow",
			script:   exit0,
			wantDeny: false,
			wantMsg:  "permitted",
		},
		{
			name:     "exit_2_deny",
			script:   exit2,
			wantDeny: true,
			wantMsg:  "Operation blocked by policy",
		},
		{
			name:     "exit_1_error_default_allow",
			script:   exit1,
			wantDeny: false,
			wantMsg:  "Hook error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
				hooks.PreToolUse: {{
					Matcher: "*",
					Hooks: []hooks.EntryConfig{
						{Type: "command", Command: "sh " + tc.script},
					},
				}},
			})

			results, _ := r.Fire(
				context.Background(), hooks.PreToolUse,
				hooks.Input{Event: hooks.PreToolUse, ToolName: "Bash"},
			)
			if len(results) == 0 {
				t.Fatal("Expected result")
			}

			if hooks.HasDeny(results) != tc.wantDeny {
				t.Errorf("deny=%v, want %v", hooks.HasDeny(results), tc.wantDeny)
			}

			msgs := hooks.Messages(results)
			found := false
			for _, m := range msgs {
				if strings.Contains(m, tc.wantMsg) {
					found = true
				}
			}
			if !found {
				t.Errorf("message %q not found in %v", tc.wantMsg, msgs)
			}
		})
	}
}

// =====================================================================
// Integration: hooks + engine with mock provider
// =====================================================================

func TestClaudeCode_HookDenyBlocksToolInEngine(t *testing.T) {
	dir := t.TempDir()
	denyScript := filepath.Join(dir, "block-all.sh")
	os.WriteFile(denyScript, []byte(
		"#!/bin/sh\necho 'hook blocked' >&2\nexit 2\n",
	), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PreToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + denyScript}},
		}},
	})

	var mu sync.Mutex
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := idx
		idx++
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if i == 0 {
			// First call: model requests a tool
			w.Write([]byte(ccToolSSE("t1", "bash", `{"command":"echo hi"}`)))
		} else {
			// After hook deny, model should get text response
			w.Write([]byte(ccTextSSE("Understood, skipping.")))
		}
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{
		APIKey:  "test",
		BaseURL: srv.URL,
	}

	eng, err := engine.New(engine.EngineParams{
		Config: cfg,
		Hooks:  hookRunner,
	})
	if err != nil {
		t.Fatal(err)
	}

	events := ccDrain(eng.Run(context.Background(), "do something"))
	hasDone := false
	for _, ev := range events {
		if ev.Type == event.Done {
			hasDone = true
		}
	}
	if !hasDone {
		t.Error("Engine should complete even when hooks deny")
	}
}

func TestClaudeCode_PostToolUseHookInEngine(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "post.log")
	script := filepath.Join(dir, "post.sh")
	os.WriteFile(script, []byte(fmt.Sprintf(`#!/bin/sh
echo "post_hook_fired" >> %s
echo '{"decision":"allow"}'
`, logFile)), 0o755)

	hookRunner := hooks.NewRunner(map[hooks.Event][]hooks.MatcherConfig{
		hooks.PostToolUse: {{
			Matcher: "*",
			Hooks:   []hooks.EntryConfig{{Type: "command", Command: "sh " + script}},
		}},
	})

	var mu sync.Mutex
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := idx
		idx++
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if i == 0 {
			w.Write([]byte(ccToolSSE("t1", "ls", `{"path":"."}`)))
		} else {
			w.Write([]byte(ccTextSSE("Done listing.")))
		}
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.Provider["anthropic"] = config.ProviderConfig{
		APIKey:  "test",
		BaseURL: srv.URL,
	}

	eng, _ := engine.New(engine.EngineParams{
		Config: cfg,
		Hooks:  hookRunner,
	})
	_ = ccDrain(eng.Run(context.Background(), "list"))

	data, _ := os.ReadFile(logFile)
	if !strings.Contains(string(data), "post_hook_fired") {
		t.Error("PostToolUse hook should fire after tool execution")
	}
}

// =====================================================================
// Helpers
// =====================================================================

func ccTextSSE(text string) string {
	return fmt.Sprintf(
		"event: content_block_start\ndata: %s\n\n"+
			"event: content_block_delta\ndata: %s\n\n"+
			"event: content_block_stop\ndata: {}\n\n"+
			"event: message_stop\ndata: {}\n\n",
		`{"index":0,"content_block":{"type":"text","text":""}}`,
		fmt.Sprintf(`{"delta":{"type":"text_delta","text":%q}}`, text),
	)
}

func ccToolSSE(id, name, input string) string {
	return fmt.Sprintf(
		"event: content_block_start\ndata: %s\n\n"+
			"event: content_block_delta\ndata: %s\n\n"+
			"event: content_block_stop\ndata: {}\n\n"+
			"event: message_stop\ndata: {}\n\n",
		fmt.Sprintf(`{"index":0,"content_block":{"type":"tool_use","id":%q,"name":%q}}`, id, name),
		fmt.Sprintf(`{"delta":{"type":"input_json_delta","partial_json":%q}}`, input),
	)
}

func ccDrain(ch <-chan event.Event) []event.Event {
	var out []event.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}
