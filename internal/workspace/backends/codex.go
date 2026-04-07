package backends

import (
	"context"
	"path/filepath"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// CodexBackend implements workspace.Agent for OpenAI Codex CLI.
type CodexBackend struct{}

func (b *CodexBackend) Name() string { return "codex" }

func (b *CodexBackend) LaunchCommand(sess *workspace.AgentSession) ([]string, error) {
	args := []string{"exec", "--dangerously-bypass-approvals-and-sandbox"}
	if sess.Model != "" {
		args = append(args, "--model", sess.Model)
	}
	args = append(args, sess.Task)
	return append([]string{"codex"}, args...), nil
}

// RestoreCommand returns nil; Codex CLI has no session resume.
func (b *CodexBackend) RestoreCommand(_ *workspace.AgentSession) ([]string, error) {
	return nil, nil
}

func (b *CodexBackend) Environment(sess *workspace.AgentSession) (map[string]string, error) {
	codexHome := filepath.Join(sess.WorkspacePath, "codex-home")
	return map[string]string{
		"ALTCODE_SESSION_ID": sess.AOSessionID,
		"ALTCODE_ROLE":       sess.Role,
		"CODEX_HOME":         codexHome,
	}, nil
}

func (b *CodexBackend) SetupWorkspaceHooks(sess *workspace.AgentSession) error {
	return installPathWrappers(sess.WorkspacePath)
}

func (b *CodexBackend) ActivityState(
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
	if entry, err := readLastJSONLEntry(jsonlPath); err == nil {
		if state := checkActionableState(entry); state != nil {
			return state, nil
		}
	}
	return jsonlFallbackState(jsonlPath, ActiveWindow, ReadyThreshold)
}

func (b *CodexBackend) IsProcessRunning(sess *workspace.AgentSession) (bool, error) {
	if sess.RuntimeHandleID == "" {
		return false, nil
	}
	return checkPID(sess.RuntimeHandleID)
}

func (b *CodexBackend) SessionInfo(
	_ context.Context, _ *workspace.AgentSession,
) (*workspace.AgentSessionInfo, error) {
	return nil, nil
}
