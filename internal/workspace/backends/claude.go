package backends

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// ClaudeBackend implements workspace.Agent for the Claude Code CLI.
type ClaudeBackend struct{}

func (b *ClaudeBackend) Name() string { return "claude" }

func (b *ClaudeBackend) LaunchCommand(sess *workspace.AgentSession) ([]string, error) {
	args := []string{
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
		"--max-turns", fmt.Sprintf("%d", maxTurns(sess.MaxTurns, 50)),
	}
	if sess.Model != "" {
		args = append(args, "--model", sess.Model)
	}
	if sess.SystemPromptAppend != "" {
		args = append(args, "--append-system-prompt", sess.SystemPromptAppend)
	}
	if sess.PriorSessionID != "" {
		args = append(args, "--resume", sess.PriorSessionID)
	}
	args = append(args, "--print", sess.Task)
	return append([]string{"claude"}, args...), nil
}

func (b *ClaudeBackend) RestoreCommand(sess *workspace.AgentSession) ([]string, error) {
	if sess.PriorSessionID == "" {
		return nil, nil
	}
	return b.LaunchCommand(sess)
}

func (b *ClaudeBackend) Environment(sess *workspace.AgentSession) (map[string]string, error) {
	return map[string]string{
		"ALTCODE_SESSION_ID": sess.AOSessionID,
		"ALTCODE_ROLE":       sess.Role,
	}, nil
}

func (b *ClaudeBackend) SetupWorkspaceHooks(sess *workspace.AgentSession) error {
	settingsPath := filepath.Join(sess.WorktreePath, ".claude", "settings.json")
	return writeClaudeHooks(settingsPath, sess)
}

func (b *ClaudeBackend) ActivityState(
	ctx context.Context, sess *workspace.AgentSession,
) (*workspace.ActivityDetection, error) {
	alive, err := b.IsProcessRunning(sess)
	if err != nil || !alive {
		return &workspace.ActivityDetection{
			State:     workspace.ActivityExited,
			Timestamp: time.Now(),
			Source:    "process_dead",
		}, nil
	}
	jsonlPath := filepath.Join(sess.WorktreePath, ".claude", "activity.jsonl")
	if entry, err := readLastJSONLEntry(jsonlPath); err == nil {
		if state := checkActionableState(entry); state != nil {
			return state, nil
		}
	}
	return jsonlFallbackState(jsonlPath, ActiveWindow, ReadyThreshold)
}

func (b *ClaudeBackend) IsProcessRunning(sess *workspace.AgentSession) (bool, error) {
	if sess.RuntimeHandleID == "" {
		return false, nil
	}
	return checkPID(sess.RuntimeHandleID)
}

func (b *ClaudeBackend) SessionInfo(
	ctx context.Context, sess *workspace.AgentSession,
) (*workspace.AgentSessionInfo, error) {
	jsonlPath := filepath.Join(sess.WorktreePath, ".claude", "activity.jsonl")
	return parseClaudeSessionInfo(jsonlPath)
}
