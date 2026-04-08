package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/agent"
)

// maxWorkerOutputBytes caps each worker's output in the manager prompt.
const maxWorkerOutputBytes = 8 * 1024

// ManagerConfig configures the manager agent synthesis step.
type ManagerConfig struct {
	Task          string
	WorkerOutputs map[string]string // role -> output text
	GitRoot       string
	WorkDir       string // manager's working directory
	Backend       string // "codex" or "claude"
}

// ManagerResult holds the manager agent's synthesis.
type ManagerResult struct {
	Summary      string
	FilesChanged []string
	Output       string
}

// RunManager spawns a manager agent that synthesizes worker outputs.
func RunManager(
	ctx context.Context, cfg ManagerConfig,
) (*ManagerResult, error) {
	prompt := BuildManagerPrompt(cfg.Task, cfg.WorkerOutputs)

	backend := agent.BackendCodex
	if cfg.Backend == "claude" {
		backend = agent.BackendClaude
	}

	stream := agent.SpawnExternal(ctx, agent.ExternalAgentConfig{
		Backend: backend,
		Role:    "manager",
		WorkDir: cfg.WorkDir,
	}, prompt)

	// Drain lines (we only need the final result).
	for range stream.Lines {
	}
	res := <-stream.Result

	if res.Error != nil {
		return nil, fmt.Errorf("manager agent: %w", res.Error)
	}

	return &ManagerResult{
		Summary: truncateStr(res.Output, 2048),
		Output:  res.Output,
	}, nil
}

// BuildManagerPrompt constructs the prompt sent to the manager agent.
func BuildManagerPrompt(
	task string, workerOutputs map[string]string,
) string {
	var b strings.Builder
	b.WriteString(
		"You are the manager agent. " +
			"Your workers completed these tasks:\n\n")
	b.WriteString("Original task: " + task + "\n\n")

	for role, output := range workerOutputs {
		b.WriteString("=== " + role + " ===\n")
		b.WriteString(TruncateWorkerOutput(output))
		b.WriteString("\n\n")
	}

	b.WriteString(
		"Merge all changes into a coherent result. " +
			"Resolve any conflicts between workers. " +
			"List the final files changed.")
	return b.String()
}

// TruncateWorkerOutput caps output at maxWorkerOutputBytes.
func TruncateWorkerOutput(s string) string {
	if len(s) <= maxWorkerOutputBytes {
		return s
	}
	return s[:maxWorkerOutputBytes] + "\n[...truncated]"
}

// truncateStr limits a string to n bytes.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
