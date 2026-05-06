package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/config"
	"github.com/jiayaoqijia/altcode/internal/lifecycle"
	"github.com/jiayaoqijia/altcode/internal/scm"
	"github.com/jiayaoqijia/altcode/internal/workspace"
	"github.com/spf13/cobra"
)

// --- altcode workspace spawn <role> ---

func workspaceSpawnCmd() *cobra.Command {
	var backend, model string
	cmd := &cobra.Command{
		Use:   "spawn <role>",
		Short: "Spawn an additional agent into the active workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			role := args[0]
			st := openWorkspaceStore()
			id, err := mostRecentWorkspaceID(st)
			if err != nil {
				return err
			}
			return spawnAgent(st, id, role, backend, model)
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "claude",
		"Backend for the new agent")
	cmd.Flags().StringVarP(&model, "model", "m", "",
		"Model override for the agent")
	return cmd
}

func spawnAgent(
	st *workspace.Store, id, role, backend, model string,
) error {
	sess, err := st.LoadSession(id)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	if _, exists := sess.Agents[role]; exists {
		return fmt.Errorf(
			"role %q already exists in workspace %s", role, id)
	}
	shortID := sess.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	branch := workspace.BranchName(shortID, role, sess.Task)
	sess.Agents[role] = &workspace.AgentRecord{
		Role:          role,
		Backend:       backend,
		Model:         model,
		Branch:        branch,
		ActivityState: workspace.ActivitySpawning,
		CIStatus:      workspace.CIUnknown,
		ReviewStatus:  workspace.ReviewNone,
	}
	if err := st.SaveSession(sess); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	fmt.Printf("Agent %s (%s) added to workspace %s\n",
		role, backend, id)
	return nil
}

// --- altcode workspace send <role> <message> ---

func workspaceSendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send <role> <message>",
		Short: "Send a message to a specific agent",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			role := args[0]
			message := strings.Join(args[1:], " ")
			st := openWorkspaceStore()
			id, err := mostRecentWorkspaceID(st)
			if err != nil {
				return err
			}
			ctx := context.Background()
			if err := st.SendMessage(ctx, id, role, message); err != nil {
				return err
			}
			fmt.Printf("Message sent to %s in workspace %s\n",
				role, id)
			return nil
		},
	}
}

// --- altcode workspace review-check [id] ---

func workspaceReviewCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review-check [id]",
		Short: "Manually trigger a CI/review poll cycle",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st := openWorkspaceStore()
			id, err := resolveWorkspaceID(st, args)
			if err != nil {
				return err
			}
			return advanceOnce(st, id)
		},
	}
}

func advanceOnce(st *workspace.Store, id string) error {
	sess, err := st.LoadSession(id)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	plugins := workspace.PluginSet{
		SCM:      &scm.NoopSCM{},
		Notifier: &noopNotifier{},
	}
	log := slog.New(slog.NewTextHandler(
		os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	mgr := lifecycle.NewManager(st, plugins, log)
	ctx := context.Background()
	if err := mgr.Advance(ctx, sess); err != nil {
		return fmt.Errorf("advance: %w", err)
	}
	if err := st.SaveSession(sess); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	fmt.Printf("Workspace %s advanced (status: %s)\n",
		id, sess.Status)
	return nil
}

// --- altcode workspace rollback --turn N [id] ---

func workspaceRollbackCmd() *cobra.Command {
	var turn int
	cmd := &cobra.Command{
		Use:   "rollback [id]",
		Short: "Rollback a workspace to a specific turn checkpoint",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if turn <= 0 {
				return fmt.Errorf(
					"--turn is required and must be positive")
			}
			st := openWorkspaceStore()
			id, err := resolveWorkspaceID(st, args)
			if err != nil {
				return err
			}
			return rollbackToTurn(st, id, turn)
		},
	}
	cmd.Flags().IntVar(&turn, "turn", 0,
		"Turn number to rollback to (required)")
	return cmd
}

func rollbackToTurn(
	st *workspace.Store, id string, turn int,
) error {
	wd, _ := os.Getwd()
	root := config.DetectProjectRoot(wd)
	wsDir := filepath.Join(root, ".altcode", "workspace")
	sess, err := st.LoadSession(id)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	cpFile := filepath.Join(
		wsDir, id, fmt.Sprintf("checkpoint-%d.sha", turn))
	data, err := os.ReadFile(cpFile)
	if err != nil {
		return fmt.Errorf(
			"checkpoint for turn %d not found: %w", turn, err)
	}
	sha := strings.TrimSpace(string(data))
	ctx := context.Background()
	for _, rec := range sess.Agents {
		if rec.WorktreePath == "" {
			continue
		}
		if _, gerr := runGitInDir(
			ctx, rec.WorktreePath, "reset", "--hard", sha,
		); gerr != nil {
			return fmt.Errorf("reset %s: %w", rec.Role, gerr)
		}
		rec.TurnCount = turn
	}
	sess.Status = workspace.WSSWorking
	if err := st.SaveSession(sess); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	fmt.Printf("Workspace %s rolled back to turn %d (%s)\n",
		id, turn, sha[:8])
	return nil
}

// runGitInDir executes a git command in the given directory.
func runGitInDir(
	ctx context.Context, dir string, args ...string,
) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"git %s: %s: %w", strings.Join(args, " "),
			strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// --- altcode workspace init ---

func workspaceInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize workspace config and directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, _ := os.Getwd()
			root := config.DetectProjectRoot(wd)
			return initWorkspaceDir(root)
		},
	}
}

func initWorkspaceDir(root string) error {
	dirs := []string{
		filepath.Join(root, ".altcode", "workflows"),
		filepath.Join(root, ".altcode", "workspace"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	cfgPath := filepath.Join(root, ".altcode", "config.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("Config already exists: %s\n", cfgPath)
		return nil
	}
	defaultCfg := "# altcode workspace configuration\n" +
		"base_branch: main\n" +
		"max_ci_retries: 3\n" +
		"merge_method: squash\n" +
		"auto_merge: true\n"
	if err := os.WriteFile(
		cfgPath, []byte(defaultCfg), 0o644,
	); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	fmt.Printf("Initialized workspace at %s\n",
		filepath.Join(root, ".altcode"))
	return nil
}

// --- shared helpers ---

// openWorkspaceStore returns a Store rooted at .altcode/workspace/.
func openWorkspaceStore() *workspace.Store {
	wd, _ := os.Getwd()
	root := config.DetectProjectRoot(wd)
	wsDir := filepath.Join(root, ".altcode", "workspace")
	return workspace.NewStore(wsDir)
}

// resolveWorkspaceID returns the explicit ID from args or the most
// recent non-terminal workspace. Returns an error if none exist.
func resolveWorkspaceID(
	st *workspace.Store, args []string,
) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	return mostRecentWorkspaceID(st)
}

// mostRecentWorkspaceID finds the workspace with the newest
// UpdatedAt timestamp among non-terminal sessions.
func mostRecentWorkspaceID(
	st *workspace.Store,
) (string, error) {
	ids, err := st.ListSessions()
	if err != nil || len(ids) == 0 {
		return "", fmt.Errorf(
			"no workspaces found; start one with: " +
				"altcode workspace \"task\"")
	}
	var bestID string
	var bestTime time.Time
	for _, id := range ids {
		sess, lerr := st.LoadSession(id)
		if lerr != nil {
			continue
		}
		if isTerminalStatus(sess.Status) {
			continue
		}
		if sess.UpdatedAt.After(bestTime) {
			bestID = id
			bestTime = sess.UpdatedAt
		}
	}
	if bestID == "" {
		return "", fmt.Errorf(
			"all workspaces are completed; " +
				"start a new one with: altcode workspace \"task\"")
	}
	return bestID, nil
}
