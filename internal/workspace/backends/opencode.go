package backends

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

// OpenCodeBackend implements workspace.Agent for the OpenCode CLI.
type OpenCodeBackend struct{}

func (b *OpenCodeBackend) Name() string { return "opencode" }

func (b *OpenCodeBackend) LaunchCommand(sess *workspace.AgentSession) ([]string, error) {
	args := []string{"exec"}
	if sess.Model != "" {
		args = append(args, "--model", sess.Model)
	}
	if sess.PriorSessionID != "" {
		args = append(args, "--session", sess.PriorSessionID)
	}
	if sess.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", sess.MaxTurns))
	}
	args = append(args, sess.Task)
	return append([]string{"opencode"}, args...), nil
}

func (b *OpenCodeBackend) RestoreCommand(sess *workspace.AgentSession) ([]string, error) {
	if sess.PriorSessionID == "" {
		return nil, nil
	}
	return b.LaunchCommand(sess)
}

func (b *OpenCodeBackend) Environment(sess *workspace.AgentSession) (map[string]string, error) {
	return map[string]string{
		"ALTCODE_SESSION_ID": sess.AOSessionID,
		"ALTCODE_ROLE":       sess.Role,
	}, nil
}

func (b *OpenCodeBackend) SetupWorkspaceHooks(sess *workspace.AgentSession) error {
	return installPathWrappers(sess.WorkspacePath)
}

func (b *OpenCodeBackend) ActivityState(
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
	// Check JSONL for actionable states (waiting_input/blocked) before fallback
	jsonlPath := filepath.Join(sess.WorkspacePath, "agents", sess.Role+".jsonl")
	if entry, err := readLastJSONLEntry(jsonlPath); err == nil {
		if state := checkActionableState(entry); state != nil {
			return state, nil
		}
	}
	return jsonlFallbackState(jsonlPath, ActiveWindow, ReadyThreshold)
}

func (b *OpenCodeBackend) IsProcessRunning(sess *workspace.AgentSession) (bool, error) {
	if sess.RuntimeHandleID == "" {
		return false, nil
	}
	return checkPID(sess.RuntimeHandleID)
}

func (b *OpenCodeBackend) SessionInfo(
	_ context.Context, _ *workspace.AgentSession,
) (*workspace.AgentSessionInfo, error) {
	return nil, nil
}
