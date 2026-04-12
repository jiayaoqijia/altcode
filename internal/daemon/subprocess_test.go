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

func TestSpawnAgent_NonexistentBinary(t *testing.T) {
	_, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "/no/such/binary-abc123",
		Dir:    t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
}

func TestSpawnAgent_SendMessage(t *testing.T) {
	// cat reads stdin and writes to stdout — verifies SendMessage.
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "cat",
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	defer proc.Kill()

	if err := proc.SendMessage("ping"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	// Close stdin so cat exits and ReadAll returns.
	proc.Stdin.Close()

	output, err := proc.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if output != "ping" {
		t.Errorf("output = %q, want %q", output, "ping")
	}
}

func TestSpawnAgent_KillAlreadyExited(t *testing.T) {
	// 'true' exits immediately — Kill on an exited process must not panic.
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "true",
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	// Wait for it to exit naturally.
	_ = proc.Wait()

	// Kill after exit should not panic or error fatally.
	if err := proc.Kill(); err != nil {
		t.Errorf("Kill on exited process: %v", err)
	}
	// Double Kill must also be safe.
	if err := proc.Kill(); err != nil {
		t.Errorf("second Kill: %v", err)
	}
}

func TestSpawnAgent_IsRunningAfterWait(t *testing.T) {
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "true",
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	_ = proc.Wait()
	if proc.IsRunning() {
		t.Error("IsRunning should be false after Wait")
	}
}

func TestSpawnAgent_KillIdempotent(t *testing.T) {
	// Rapid concurrent Kill calls must not panic.
	proc, err := SpawnAgent(context.Background(), AgentConfig{
		Binary: "sleep",
		Args:   []string{"60"},
		Dir:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	// Kill three times in sequence — no panic, no double-close.
	_ = proc.Kill()
	_ = proc.Kill()
	_ = proc.Kill()
}
