package backends

import (
	"context"
	"path/filepath"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// AiderBackend implements workspace.Agent for the Aider CLI.
type AiderBackend struct{}

func (b *AiderBackend) Name() string { return "aider" }

func (b *AiderBackend) LaunchCommand(sess *workspace.AgentSession) ([]string, error) {
	args := []string{"--no-auto-commits"}
	if sess.Model != "" {
		args = append(args, "--model", sess.Model)
	}
	args = append(args, sess.Task)
	return append([]string{"aider"}, args...), nil
}

// RestoreCommand returns nil; Aider has no session resume.
func (b *AiderBackend) RestoreCommand(_ *workspace.AgentSession) ([]string, error) {
	return nil, nil
}

func (b *AiderBackend) Environment(sess *workspace.AgentSession) (map[string]string, error) {
	return map[string]string{
		"ALTCODE_SESSION_ID": sess.AOSessionID,
		"ALTCODE_ROLE":       sess.Role,
	}, nil
}

func (b *AiderBackend) SetupWorkspaceHooks(sess *workspace.AgentSession) error {
	return installPathWrappers(sess.WorkspacePath)
}

func (b *AiderBackend) ActivityState(
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
	jsonlPath := filepath.Join(sess.WorkspacePath, "agents", sess.Role+".jsonl")
	return jsonlFallbackState(jsonlPath, ActiveWindow, ReadyThreshold)
}

func (b *AiderBackend) IsProcessRunning(sess *workspace.AgentSession) (bool, error) {
	if sess.RuntimeHandleID == "" {
		return false, nil
	}
	return checkPID(sess.RuntimeHandleID)
}

func (b *AiderBackend) SessionInfo(
	_ context.Context, _ *workspace.AgentSession,
) (*workspace.AgentSessionInfo, error) {
	return nil, nil
}
