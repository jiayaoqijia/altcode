package agent

import (
	"context"
	"testing"
	"time"
)

func TestDetectAvailableBackends(t *testing.T) {
	backends := DetectAvailableBackends()
	// Just verify it doesn't panic — actual availability depends on system
	t.Logf("Available backends: %v", backends)
}

func TestBuildCLICommand_Codex(t *testing.T) {
	ctx := context.Background()
	cmd := buildCLICommand(ctx, ExternalAgentConfig{
		Backend: BackendCodex,
		Model:   "o3",
	}, "fix the bug")

	if cmd.Path == "" {
		t.Skip("codex not on PATH")
	}
	args := cmd.Args
	if len(args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", args)
	}
	// args[0] is the binary name
	if args[1] != "exec" {
		t.Errorf("expected 'exec', got %q", args[1])
	}
}

func TestBuildCLICommand_Claude(t *testing.T) {
	ctx := context.Background()
	cmd := buildCLICommand(ctx, ExternalAgentConfig{
		Backend: BackendClaude,
		Model:   "claude-sonnet-4-20250514",
	}, "review this code")

	args := cmd.Args
	if len(args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", args)
	}
	// Should use --permission-mode bypassPermissions (not --output-format which conflicts with -p)
	if args[1] != "--permission-mode" || args[2] != "bypassPermissions" {
		t.Errorf("expected '--permission-mode bypassPermissions', got %q %q", args[1], args[2])
	}
}

func TestSpawnExternal_Timeout(t *testing.T) {
	ctx := context.Background()
	stream := SpawnExternal(ctx, ExternalAgentConfig{
		Backend: "echo",
		Role:    "test",
		Timeout: 100 * time.Millisecond,
	}, "hello")

	// Drain lines
	for range stream.Lines {
	}
	result := <-stream.Result
	t.Logf("Result: role=%s elapsed=%v err=%v", result.Role, result.Elapsed, result.Error)
}

func TestSpawnTeam(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	configs := []ExternalAgentConfig{
		{Backend: "echo", Role: "agent-1", Timeout: 500 * time.Millisecond},
		{Backend: "echo", Role: "agent-2", Timeout: 500 * time.Millisecond},
	}

	streams := SpawnTeam(ctx, configs, "test")
	if len(streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(streams))
	}

	results := WaitAll(streams, 3*time.Second)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}
