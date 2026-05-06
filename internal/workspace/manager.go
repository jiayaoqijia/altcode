package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/agent"
)

// maxWorkerOutputBytes caps each worker's output in the manager prompt.
const maxWorkerOutputBytes = 8 * 1024

// maxDiffBytes caps each worker's full diff in the manager prompt.
const maxDiffBytes = 4 * 1024

// WorkerInfo carries a worker's output plus worktree metadata
// so the manager prompt can include actual git diffs.
type WorkerInfo struct {
	Output       string // stdout/stderr text
	WorktreePath string // git worktree root (empty = no diff)
	BaseBranch   string // e.g. "main"
}

// ManagerConfig configures the manager agent synthesis step.
type ManagerConfig struct {
	Task          string
	WorkerOutputs map[string]string // role -> output text (legacy)
	Workers       map[string]WorkerInfo
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
	prompt := BuildManagerPrompt(
		ctx, cfg.Task, cfg.WorkerOutputs, cfg.Workers)

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

// BuildManagerPrompt constructs the prompt sent to the manager
// agent. When Workers is populated, it includes git diff stat and
// full diff (capped at maxDiffBytes) so the manager has factual
// grounding about what each worker actually changed.
func BuildManagerPrompt(
	ctx context.Context,
	task string,
	workerOutputs map[string]string,
	workers map[string]WorkerInfo,
) string {
	var b strings.Builder
	b.WriteString(
		"You are the manager agent. " +
			"Your workers completed these tasks:\n\n")
	b.WriteString("Original task: " + task + "\n\n")

	// Prefer Workers (has diffs), fall back to legacy map.
	if len(workers) > 0 {
		for role, w := range workers {
			writeWorkerSection(
				ctx, &b, role, w)
		}
	} else {
		for role, output := range workerOutputs {
			b.WriteString("=== " + role + " ===\n")
			b.WriteString(TruncateWorkerOutput(output))
			b.WriteString("\n\n")
		}
	}

	b.WriteString(
		"Merge all changes into a coherent result. " +
			"Resolve any conflicts between workers. " +
			"List the final files changed.")
	return b.String()
}

// writeWorkerSection writes one worker's output + git diffs.
func writeWorkerSection(
	ctx context.Context,
	b *strings.Builder,
	role string,
	w WorkerInfo,
) {
	b.WriteString("=== " + role + " ===\n")
	b.WriteString(TruncateWorkerOutput(w.Output))
	b.WriteString("\n")

	if w.WorktreePath == "" || w.BaseBranch == "" {
		b.WriteString("\n")
		return
	}

	base := w.BaseBranch
	stat, err := runGit(
		ctx, w.WorktreePath,
		"diff", base+"..HEAD", "--stat")
	if err == nil && strings.TrimSpace(stat) != "" {
		b.WriteString("\nDiff stat:\n")
		b.WriteString(strings.TrimSpace(stat))
		b.WriteString("\n")
	}

	full, err := runGit(
		ctx, w.WorktreePath,
		"diff", base+"..HEAD")
	if err == nil && strings.TrimSpace(full) != "" {
		b.WriteString("\nDiff:\n")
		b.WriteString(truncateDiff(full))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// truncateDiff caps a diff string at maxDiffBytes.
func truncateDiff(s string) string {
	if len(s) <= maxDiffBytes {
		return s
	}
	return s[:maxDiffBytes] + "\n[...diff truncated]"
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
