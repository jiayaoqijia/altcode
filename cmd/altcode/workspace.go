package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/lifecycle"
	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/altcode-ai/altcode/internal/workspace/backends"
	"github.com/altcode-ai/altcode/internal/wsctl"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

func init() {
	// Registered by main.go's root command via addWorkspaceCmd.
}

// addWorkspaceCmd wires the "workspace" (alias "ws") subcommand tree
// into the given root cobra command.
func addWorkspaceCmd(root *cobra.Command) {
	var (
		wsBase    string
		wsAgents  string
		wsModel   string
		wsDryRun  bool
		wsNoPR    bool
		wsCfgPath string
	)

	wsCmd := &cobra.Command{
		Use:     "workspace [task]",
		Aliases: []string{"ws"},
		Short:   "Multi-agent workspace orchestration",
		Long: `Start, monitor, and manage multi-agent workspaces.

  altcode workspace "add JWT auth"               Start a workspace
  altcode ws "fix bug" --agents claude,codex      Choose backends
  altcode workspace status [id]                   Show workspace status
  altcode workspace list                          List all workspaces
  altcode workspace resume [id]                   Resume a saved workspace`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			task := strings.Join(args, " ")
			return runWorkspaceStart(
				task, wsBase, wsAgents, wsModel,
				wsDryRun, wsNoPR, wsCfgPath,
			)
		},
	}

	wsCmd.Flags().StringVarP(&wsBase, "base", "b", "",
		"Base branch (default: main)")
	wsCmd.Flags().StringVarP(&wsAgents, "agents", "a", "",
		"Comma-separated backends (default: auto-detect)")
	wsCmd.Flags().StringVarP(&wsModel, "model", "m", "",
		"Model override for all agents")
	wsCmd.Flags().BoolVar(&wsDryRun, "dry-run", false,
		"Print plan without executing")
	wsCmd.Flags().BoolVar(&wsNoPR, "no-pr", false,
		"Skip PR creation")
	wsCmd.Flags().StringVar(&wsCfgPath, "config", "",
		"Config path override")

	// Subcommands
	wsCmd.AddCommand(workspaceStatusCmd())
	wsCmd.AddCommand(workspaceListCmd())
	wsCmd.AddCommand(workspaceResumeCmd())

	root.AddCommand(wsCmd)
}

// --- altcode workspace "task" ---

func runWorkspaceStart(
	task, base, agentsFlag, model string,
	dryRun, noPR bool,
	cfgPath string,
) error {
	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt)
	defer stop()

	wd, _ := os.Getwd()
	gitRoot := config.DetectProjectRoot(wd)
	if base == "" {
		base = "main"
	}

	// Detect backends
	available, err := backends.DetectBackends(ctx)
	if err != nil {
		return fmt.Errorf("detect backends: %w", err)
	}
	selected := filterBackends(available, agentsFlag)
	if len(selected) == 0 {
		return fmt.Errorf(
			"no coding-agent backends found on PATH " +
				"(install claude, codex, opencode, or aider)")
	}

	// Generate workspace ID
	id := ulid.MustNew(
		ulid.Timestamp(time.Now()),
		ulid.Monotonic(rand.Reader, 0),
	).String()
	shortID := id[:8]

	// Build session
	sess := &workspace.WorkspaceSession{
		ID:           id,
		Task:         task,
		Status:       workspace.WSSSpawning,
		GitRoot:      gitRoot,
		BaseBranch:   base,
		Agents:       make(map[string]*workspace.AgentRecord),
		MaxCIRetries: 3,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		AutoMerge:    !noPR,
		MergeMethod:  workspace.MergeSquash,
	}

	roles := assignRoles(selected)
	for _, r := range roles {
		branch := workspace.BranchName(
			shortID, r.role, task)
		sess.Agents[r.role] = &workspace.AgentRecord{
			Role:          r.role,
			Backend:       r.backend,
			Model:         model,
			Branch:        branch,
			ActivityState: workspace.ActivitySpawning,
			CIStatus:      workspace.CIUnknown,
			ReviewStatus:  workspace.ReviewNone,
		}
	}

	if dryRun {
		return printDryRun(sess, selected)
	}

	// Create workspace directory and store
	wsDir := filepath.Join(gitRoot, ".altcode", "workspace")
	store := workspace.NewStore(wsDir)
	if err := store.SaveSession(sess); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	// Create worktrees and inject context
	wksp := workspace.NewWorktreeWorkspace()
	home, _ := os.UserHomeDir()
	for _, rec := range sess.Agents {
		wtPath := filepath.Join(
			home, ".altcode", "worktrees", id, rec.Role)
		result, serr := wksp.Setup(ctx,
			workspace.WorkspaceSetupRequest{
				GitRoot:      gitRoot,
				WorktreePath: wtPath,
				Branch:       rec.Branch,
				BaseRef:      base,
			})
		if serr != nil {
			return fmt.Errorf(
				"setup worktree %s: %w", rec.Role, serr)
		}
		rec.WorktreePath = result.Path

		// Inject shared context
		if ierr := wsctl.InjectWorkspaceContext(
			wtPath, rec.Backend, rec.Role, task, sess,
		); ierr != nil {
			return fmt.Errorf(
				"inject context %s: %w", rec.Role, ierr)
		}
	}

	// Spawn agents
	plugins := buildPluginSet(selected)
	for _, rec := range sess.Agents {
		agent, ok := plugins.Agents[rec.Backend]
		if !ok {
			continue
		}

		as := &workspace.AgentSession{
			WorkspacePath: filepath.Join(wsDir, id),
			WorktreePath:  rec.WorktreePath,
			Branch:        rec.Branch,
			Task:          task,
			Role:          rec.Role,
			Model:         rec.Model,
			Env:           os.Environ(),
			AOSessionID:   id,
		}

		// Setup hooks for metadata capture
		_ = agent.SetupWorkspaceHooks(as)

		argv, cerr := agent.LaunchCommand(as)
		if cerr != nil {
			return fmt.Errorf("launch cmd %s: %w", rec.Role, cerr)
		}

		handle, serr := plugins.Runtime.Spawn(
			ctx, argv, as.Env, rec.WorktreePath)
		if serr != nil {
			return fmt.Errorf("spawn %s: %w", rec.Role, serr)
		}
		rec.RuntimeHandleID = handle.ID
		rec.SpawnedAt = handle.StartedAt
	}

	if err := store.SaveSession(sess); err != nil {
		return fmt.Errorf("save session after spawn: %w", err)
	}

	// Start lifecycle manager
	log := slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelWarn}))
	mgr := lifecycle.NewManager(store, plugins, log)
	go func() {
		if rerr := mgr.Run(ctx, sess); rerr != nil {
			log.Error("lifecycle", "err", rerr)
		}
	}()

	fmt.Printf("Workspace %s started\n", id)
	fmt.Printf("Task: %s\n", task)
	fmt.Printf("Agents: %d\n", len(sess.Agents))
	for _, rec := range sess.Agents {
		fmt.Printf("  %s (%s) -> %s\n",
			rec.Role, rec.Backend, rec.Branch)
	}

	// Block until context cancelled or lifecycle completes
	<-ctx.Done()
	return nil
}

// --- altcode workspace status [id] ---

func workspaceStatusCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status [id]",
		Short: "Show workspace status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, _ := os.Getwd()
			root := config.DetectProjectRoot(wd)
			wsDir := filepath.Join(
				root, ".altcode", "workspace")
			store := workspace.NewStore(wsDir)

			if len(args) == 1 {
				return showStatus(store, args[0], jsonOut)
			}
			return listAll(store, false, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"JSON output")
	return cmd
}

func showStatus(
	store *workspace.Store, id string, jsonOut bool,
) error {
	sess, err := store.LoadSession(id)
	if err != nil {
		return fmt.Errorf("load session %s: %w", id, err)
	}

	if jsonOut {
		data, _ := json.MarshalIndent(sess, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Workspace: %s\n", sess.ID)
	fmt.Printf("Task:      %s\n", sess.Task)
	fmt.Printf("Status:    %s\n", sess.Status)
	fmt.Printf("Created:   %s\n",
		sess.CreatedAt.Format(time.RFC3339))

	if len(sess.Agents) > 0 {
		fmt.Println("\nAgents:")
		fmt.Printf("  %-14s %-10s %-12s %-8s %-8s %s\n",
			"Role", "Backend", "Activity",
			"CI", "PR", "Cost")
		for _, rec := range sess.Agents {
			pr := "--"
			if rec.PRID > 0 {
				pr = fmt.Sprintf("#%d", rec.PRID)
			}
			fmt.Printf("  %-14s %-10s %-12s %-8s %-8s $%.2f\n",
				rec.Role, rec.Backend, rec.ActivityState,
				rec.CIStatus, pr, rec.CostUSD)
		}
	}
	return nil
}

// --- altcode workspace list ---

func workspaceListCmd() *cobra.Command {
	var (
		showAll bool
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, _ := os.Getwd()
			root := config.DetectProjectRoot(wd)
			wsDir := filepath.Join(
				root, ".altcode", "workspace")
			store := workspace.NewStore(wsDir)
			return listAll(store, showAll, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&showAll, "all", false,
		"Include completed workspaces")
	cmd.Flags().BoolVar(&jsonOut, "json", false,
		"JSON output")
	return cmd
}

func listAll(
	store *workspace.Store, showAll, jsonOut bool,
) error {
	ids, err := store.ListSessions()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("No workspaces found.")
		return nil
	}

	for _, id := range ids {
		sess, lerr := store.LoadSession(id)
		if lerr != nil {
			continue
		}
		if !showAll && isTerminalStatus(sess.Status) {
			continue
		}
		if jsonOut {
			data, _ := json.Marshal(sess)
			fmt.Println(string(data))
			continue
		}
		ago := time.Since(sess.CreatedAt).Truncate(time.Second)
		agents := agentSummary(sess)
		fmt.Printf("%-12s %-15s %-30s %8s  %s\n",
			truncateID(sess.ID), sess.Status,
			truncateTask(sess.Task, 30), ago, agents)
	}
	return nil
}

// --- altcode workspace resume [id] ---

func workspaceResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume [id]",
		Short: "Resume a saved workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, _ := os.Getwd()
			root := config.DetectProjectRoot(wd)
			wsDir := filepath.Join(
				root, ".altcode", "workspace")
			store := workspace.NewStore(wsDir)

			sess, err := store.LoadSession(args[0])
			if err != nil {
				return fmt.Errorf("load: %w", err)
			}
			if isTerminalStatus(sess.Status) {
				return fmt.Errorf(
					"workspace %s is %s, cannot resume",
					sess.ID, sess.Status)
			}
			sess.Status = workspace.WSSWorking
			if err := store.SaveSession(sess); err != nil {
				return fmt.Errorf("save: %w", err)
			}
			fmt.Printf("Workspace %s resumed (status: %s)\n",
				sess.ID, sess.Status)
			return nil
		},
	}
}

// --- helpers ---

type roleAssignment struct {
	role    string
	backend string
}

// assignRoles maps detected backends to workspace roles.
// First backend is architect, second is implementer, third is reviewer.
// If only one backend, it takes all roles.
func assignRoles(detected []backends.DetectedBackend) []roleAssignment {
	roleNames := []string{
		"architect", "implementer", "reviewer",
	}
	var out []roleAssignment
	for i, name := range roleNames {
		idx := i % len(detected)
		out = append(out, roleAssignment{
			role:    name,
			backend: detected[idx].Name,
		})
	}
	return out
}

// filterBackends narrows detection to explicitly requested backends.
func filterBackends(
	all []backends.DetectedBackend,
	filter string,
) []backends.DetectedBackend {
	if filter == "" {
		return all
	}
	wanted := make(map[string]bool)
	for _, name := range strings.Split(filter, ",") {
		wanted[strings.TrimSpace(name)] = true
	}
	var out []backends.DetectedBackend
	for _, b := range all {
		if wanted[b.Name] {
			out = append(out, b)
		}
	}
	return out
}

// buildPluginSet assembles a PluginSet from detected backends.
// Runtime uses the process runtime. Other plugins are stubs.
func buildPluginSet(
	detected []backends.DetectedBackend,
) workspace.PluginSet {
	agents := make(map[string]workspace.Agent)
	for _, d := range detected {
		agent, err := backends.NewBackend(d.Name)
		if err == nil {
			agents[d.Name] = agent
		}
	}
	return workspace.PluginSet{
		Runtime: &processRuntime{},
		Agents:  agents,
	}
}

// processRuntime is a minimal Runtime that spawns OS processes.
type processRuntime struct{}

func (p *processRuntime) Name() string { return "process" }

func (p *processRuntime) Spawn(
	ctx context.Context,
	argv []string,
	env []string,
	workdir string,
) (workspace.RuntimeHandle, error) {
	if len(argv) == 0 {
		return workspace.RuntimeHandle{},
			fmt.Errorf("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workdir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return workspace.RuntimeHandle{},
			fmt.Errorf("start: %w", err)
	}
	h := workspace.RuntimeHandle{
		ID:        fmt.Sprintf("pid:%d", cmd.Process.Pid),
		StartedAt: time.Now(),
	}
	go cmd.Wait() //nolint:errcheck
	return h, nil
}

func (p *processRuntime) Attach(
	_ context.Context, _ workspace.RuntimeHandle,
) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (p *processRuntime) Kill(
	h workspace.RuntimeHandle,
) error {
	return nil // best-effort stub
}

func (p *processRuntime) IsRunning(
	h workspace.RuntimeHandle,
) (bool, error) {
	// Parse PID from handle ID
	var pid int
	if _, err := fmt.Sscanf(h.ID, "pid:%d", &pid); err != nil {
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	// Signal 0 checks existence without killing.
	err = proc.Signal(syscall.Signal(0))
	return err == nil, nil
}

func printDryRun(
	sess *workspace.WorkspaceSession,
	detected []backends.DetectedBackend,
) error {
	fmt.Println("=== DRY RUN ===")
	fmt.Printf("Workspace ID: %s\n", sess.ID)
	fmt.Printf("Task: %s\n", sess.Task)
	fmt.Printf("Base branch: %s\n", sess.BaseBranch)
	fmt.Println("\nDetected backends:")
	for _, d := range detected {
		fmt.Printf("  %s (%s) at %s\n",
			d.Name, d.Version, d.Binary)
	}
	fmt.Println("\nAgent assignments:")
	for _, rec := range sess.Agents {
		fmt.Printf("  %s -> %s (branch: %s)\n",
			rec.Role, rec.Backend, rec.Branch)
	}
	return nil
}

func isTerminalStatus(s workspace.WorkspaceStatus) bool {
	return s == workspace.WSSDone || s == workspace.WSSFailed
}

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func truncateTask(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}

func agentSummary(sess *workspace.WorkspaceSession) string {
	var parts []string
	for _, rec := range sess.Agents {
		parts = append(parts,
			fmt.Sprintf("%s(%s)", rec.Role, rec.ActivityState))
	}
	return strings.Join(parts, " ")
}
