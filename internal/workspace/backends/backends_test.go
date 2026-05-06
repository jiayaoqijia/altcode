package backends

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

func TestClaudeBackend_LaunchCommand(t *testing.T) {
	b := &ClaudeBackend{}
	sess := &workspace.AgentSession{
		Task:     "implement auth",
		Model:    "claude-sonnet-4-20250514",
		MaxTurns: 30,
	}
	cmd, err := b.LaunchCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd, " ")
	for _, want := range []string{
		"claude",
		"--output-format stream-json",
		"--verbose",
		"--permission-mode bypassPermissions",
		"--max-turns 30",
		"--model claude-sonnet-4-20250514",
		"--print implement auth",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in: %s", want, joined)
		}
	}
}

func TestClaudeBackend_RestoreCommand(t *testing.T) {
	b := &ClaudeBackend{}

	t.Run("no prior session", func(t *testing.T) {
		sess := &workspace.AgentSession{Task: "test"}
		cmd, err := b.RestoreCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		if cmd != nil {
			t.Errorf("expected nil, got %v", cmd)
		}
	})

	t.Run("with prior session", func(t *testing.T) {
		sess := &workspace.AgentSession{
			Task:           "test",
			PriorSessionID: "abc-123",
		}
		cmd, err := b.RestoreCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd, " ")
		if !strings.Contains(joined, "--resume abc-123") {
			t.Errorf("missing --resume flag in: %s", joined)
		}
	})
}

func TestCodexBackend_LaunchCommand(t *testing.T) {
	b := &CodexBackend{}
	sess := &workspace.AgentSession{
		Task:  "fix tests",
		Model: "o3",
	}
	cmd, err := b.LaunchCommand(sess)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd, " ")
	for _, want := range []string{
		"codex",
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--model o3",
		"fix tests",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in: %s", want, joined)
		}
	}
}

func TestCheckPID(t *testing.T) {
	t.Run("current process", func(t *testing.T) {
		handle := fmt.Sprintf("pid:%d", os.Getpid())
		alive, err := checkPID(handle)
		if err != nil {
			t.Fatal(err)
		}
		if !alive {
			t.Error("expected current process to be alive")
		}
	})

	t.Run("non-pid handle returns false", func(t *testing.T) {
		// Non-pid handles (tmux, etc.) return (false, nil) — managed by their runtime.
		alive, err := checkPID("tmux:session:0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if alive {
			t.Error("expected non-pid handle to return false")
		}
		alive, err = checkPID("bad-format")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if alive {
			t.Error("expected bad format to return false")
		}
	})

	t.Run("dead pid", func(t *testing.T) {
		// PID 2147483647 is unlikely to exist.
		alive, err := checkPID("pid:2147483647")
		if err != nil {
			t.Fatal(err)
		}
		if alive {
			t.Error("expected dead pid to be not alive")
		}
	})
}

func TestMaxTurns(t *testing.T) {
	if got := maxTurns(0, 50); got != 50 {
		t.Errorf("maxTurns(0, 50) = %d, want 50", got)
	}
	if got := maxTurns(30, 50); got != 30 {
		t.Errorf("maxTurns(30, 50) = %d, want 30", got)
	}
}

func TestNewBackend(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode", "aider"} {
		b, err := NewBackend(name)
		if err != nil {
			t.Errorf("NewBackend(%q) error: %v", name, err)
		}
		if b.Name() != name {
			t.Errorf("NewBackend(%q).Name() = %q", name, b.Name())
		}
	}
	_, err := NewBackend("unknown")
	if err == nil {
		t.Error("expected error for unknown backend")
	}
}
