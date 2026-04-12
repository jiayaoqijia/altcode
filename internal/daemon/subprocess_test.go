package daemon

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSpawnAgent_EchoStdout(t *testing.T) {
	// Use 'echo' as a mock agent — verifies stdout capture.
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "echo",
		Args:   []string{"hello from agent"},
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	defer proc.Kill()

	output, err := proc.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(output, "hello from agent") {
		t.Errorf("stdout = %q, want 'hello from agent'", output)
	}
	if err := proc.Wait(); err != nil {
		t.Errorf("Wait: %v", err)
	}
}

func TestSpawnAgent_Kill(t *testing.T) {
	// Sleep subprocess that we kill — verifies process group teardown.
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "sleep",
		Args:   []string{"60"},
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if err := proc.Kill(); err != nil {
		t.Errorf("Kill: %v", err)
	}
	// Wait should return an error (killed)
	err = proc.Wait()
	if err == nil {
		t.Error("expected error from Wait after Kill")
	}
}

func TestSpawnAgent_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(), 100*time.Millisecond,
	)
	defer cancel()

	proc, err := SpawnAgent(ctx, AgentConfig{
		Binary: "sleep",
		Args:   []string{"60"},
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	// Context cancels after 100ms — process should be killed.
	err = proc.Wait()
	if err == nil {
		t.Error("expected error from context cancellation")
	}
}
