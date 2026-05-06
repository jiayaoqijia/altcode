package backends

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jiayaoqijia/altcode/internal/workspace"
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
	// 1. Set up per-task CODEX_HOME (multica pattern A9)
	codexHome := filepath.Join(sess.WorkspacePath, "codex-home")
	if err := prepareCodexHome(codexHome); err != nil {
		return err
	}
	// 2. Install PATH wrappers
	return installPathWrappers(sess.WorkspacePath)
}

// prepareCodexHome creates a per-task CODEX_HOME directory.
// auth.json is symlinked (shared), config files are copied (isolated).
// The directory is owner-only (0700) because the auth.json symlink
// and copied config files can hold API keys and credentials — the
// prior 0755 leaked metadata to other local users. altcode-TUI
// round-K adversarial review.
func prepareCodexHome(codexHome string) error {
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		return err
	}
	sharedHome := resolveSharedCodexHome()

	// Symlink auth (shared across tasks)
	authSrc := filepath.Join(sharedHome, "auth.json")
	authDst := filepath.Join(codexHome, "auth.json")
	if _, err := os.Stat(authSrc); err == nil {
		os.Remove(authDst) // remove stale
		os.Symlink(authSrc, authDst)
	}

	// Copy config files (isolated per task)
	for _, name := range []string{"config.json", "config.toml", "instructions.md"} {
		src := filepath.Join(sharedHome, name)
		dst := filepath.Join(codexHome, name)
		if _, err := os.Stat(src); err == nil {
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				copyFile(src, dst)
			}
		}
	}
	return nil
}

func resolveSharedCodexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// Preserve source mode so a 0600 codex config isn't silently
	// downgraded to 0644 when copied into the per-task CODEX_HOME.
	// Fall back to 0600 (not 0644) when Stat fails — the config
	// files in ~/.codex can carry secrets, so the safer default is
	// owner-only rather than world-readable.
	mode := os.FileMode(0o600)
	if info, err := os.Stat(src); err == nil {
		mode = info.Mode().Perm()
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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
