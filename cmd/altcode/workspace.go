package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/lifecycle"
	"github.com/altcode-ai/altcode/internal/scm"
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
		wsBase     string
		wsAgents   string
		wsModel    string
		wsDryRun   bool
		wsNoPR     bool
		wsCfgPath  string
		wsWorkflow string
		wsTmux     bool
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
				wsDryRun, wsNoPR, wsCfgPath, wsWorkflow,
				wsTmux,
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
	wsCmd.Flags().StringVar(&wsWorkflow, "workflow", "",
		"Run a named workflow definition (e.g. ship-feature)")
	wsCmd.Flags().BoolVar(&wsTmux, "tmux", false,
		"Launch agents in tmux panes with real PTYs")

	// Subcommands
	wsCmd.AddCommand(workspaceStatusCmd())
	wsCmd.AddCommand(workspaceListCmd())
	wsCmd.AddCommand(workspaceResumeCmd())
	wsCmd.AddCommand(workspaceSpawnCmd())
	wsCmd.AddCommand(workspaceSendCmd())
	wsCmd.AddCommand(workspaceReviewCheckCmd())
	wsCmd.AddCommand(workspaceRollbackCmd())
	wsCmd.AddCommand(workspaceInitCmd())

	root.AddCommand(wsCmd)
}

// --- altcode workspace "task" ---

func runWorkspaceStart(
	task, base, agentsFlag, model string,
	dryRun, noPR bool,
	cfgPath, workflowName string,
	useTmux bool,
) error {
	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt)
	defer stop()

	wd, _ := os.Getwd()
	gitRoot := config.DetectProjectRoot(wd)
	if base == "" {
		base = "main"
	}

	// Load YAML agent definitions so --agents can reference them.
	home, _ := os.UserHomeDir()
	_ = backends.LoadAgentDefsIntoRegistry(
		filepath.Join(gitRoot, ".altcode", "agents"),
		filepath.Join(home, ".config", "altcode", "agents"),
	)

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

	// Load workflow definition if specified
	if workflowName != "" {
		wfPath := filepath.Join(
			gitRoot, ".altcode", "workflows", workflowName+".yaml")
		if _, serr := os.Stat(wfPath); os.IsNotExist(serr) {
			return fmt.Errorf(
				"workflow %q not found at %s", workflowName, wfPath)
		}
	}

	// Build session
	sess := &workspace.WorkspaceSession{
		ID:           id,
		Task:         task,
		Status:       workspace.WSSSpawning,
		GitRoot:      gitRoot,
		BaseBranch:   base,
		WorkflowName: workflowName,
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

	// Initialize durable event log
	evLogPath := filepath.Join(wsDir, id, "events.jsonl")
	evLog := workspace.NewEventLog(evLogPath)

	// Create worktrees and inject context.
	// Track created worktrees for cleanup on partial failure.
	wksp := workspace.NewWorktreeWorkspace()
	var createdWorktrees []string
	setupErr := func() error {
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
			createdWorktrees = append(createdWorktrees, result.Path)
			rec.WorktreePath = result.Path

			if ierr := wsctl.InjectWorkspaceContext(
				wtPath, rec.Backend, rec.Role, task, sess,
			); ierr != nil {
				return fmt.Errorf(
					"inject context %s: %w", rec.Role, ierr)
			}
		}
		return nil
	}()
	if setupErr != nil {
		// Cleanup any worktrees created before the failure
		for _, wt := range createdWorktrees {
			wksp.Teardown(ctx, wt) //nolint:errcheck
		}
		return setupErr
	}

	// Spawn agents — choose runtime based on --tmux flag.
	var rt interface {
		workspace.Runtime
		KillAll()
	}
	if useTmux {
		rt = backends.NewTmuxRuntime("altcode-" + shortID)
	} else {
		rt = &processRuntime{}
	}
	defer rt.KillAll() // ensure cleanup on exit/Ctrl+C
	plugins := buildPluginSetFromRuntime(selected, rt, gitRoot)
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
			MaxTurns:      50,
			Env:           os.Environ(),
			AOSessionID:   id,
		}

		// Setup hooks for metadata capture
		_ = agent.SetupWorkspaceHooks(as)

		// Merge agent-specific env vars (ALTCODE_SESSION_ID, ALTCODE_ROLE, etc.)
		extra, _ := agent.Environment(as)
		for k, v := range extra {
			as.Env = append(as.Env, k+"="+v)
		}

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

		// Emit spawn event
		evLog.Emit(workspace.Event{ //nolint:errcheck
			Type:    workspace.EventAgentSpawned,
			Role:    rec.Role,
			Content: rec.Backend + " " + handle.ID,
		})

		// Stream agent output to stdout with role prefix.
		// processRuntime has OnOutput; tmux uses Attach.
		role := rec.Role
		el := evLog
		if prt, ok := rt.(*processRuntime); ok {
			prt.OnOutput(handle.ID, func(line string) {
				fmt.Printf("[%s] %s\n", role, line)
				el.Emit(workspace.Event{ //nolint:errcheck
					Type:    workspace.EventAgentOutput,
					Role:    role,
					Content: line,
				})
			})
		} else {
			go func(h workspace.RuntimeHandle, r string) {
				ch, aerr := rt.Attach(ctx, h)
				if aerr != nil {
					return
				}
				for line := range ch {
					fmt.Printf("[%s] %s\n", r, line)
					el.Emit(workspace.Event{ //nolint:errcheck
						Type:    workspace.EventAgentOutput,
						Role:    r,
						Content: line,
					})
				}
			}(handle, role)
		}
	}

	if err := store.SaveSession(sess); err != nil {
		return fmt.Errorf("save session after spawn: %w", err)
	}

	// Persist PIDs for resume (Gap 1)
	if err := savePIDs(wsDir, id, sess.Agents); err != nil {
		slog.Warn("failed to save PIDs", "err", err)
	}

	fmt.Printf("Workspace %s started\n", id)
	fmt.Printf("Task: %s\n", task)
	fmt.Printf("Agents: %d\n", len(sess.Agents))
	for _, rec := range sess.Agents {
		fmt.Printf("  %s (%s) -> %s\n",
			rec.Role, rec.Backend, rec.Branch)
	}

	// Wait for all agents to complete (one-shot model)
	fmt.Println("Waiting for agents to complete...")
	deadline := time.After(10 * time.Minute)
	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

waitLoop:
	for {
		allDone := true
		for _, rec := range sess.Agents {
			h := workspace.RuntimeHandle{
				ID: rec.RuntimeHandleID,
			}
			alive, _ := rt.IsRunning(h)
			if alive {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		select {
		case <-pollTick.C:
			continue
		case <-deadline:
			fmt.Println("Timeout waiting for agents")
			rt.KillAll()
			break waitLoop
		case <-ctx.Done():
			rt.KillAll()
			return ctx.Err()
		}
	}

	// Update agent records with exit info.
	for _, rec := range sess.Agents {
		exitCode := -1
		if prt, ok := rt.(*processRuntime); ok {
			exitCode = prt.GetExitCode(rec.RuntimeHandleID)
		} else {
			// tmux mode: pane gone means exited (code unknown).
			h := workspace.RuntimeHandle{
				ID: rec.RuntimeHandleID,
			}
			alive, _ := rt.IsRunning(h)
			if !alive {
				exitCode = 0
			}
		}
		if exitCode >= 0 {
			rec.ExitCode = exitCode
			rec.ActivityState = workspace.ActivityExited
			now := time.Now()
			rec.ExitedAt = &now
			evLog.Emit(workspace.Event{ //nolint:errcheck
				Type: workspace.EventAgentExited,
				Role: rec.Role,
				Content: fmt.Sprintf(
					"exit %d", exitCode),
			})
		}
	}
	sess.Status = workspace.WSSWorking

	// Run one lifecycle advance to handle the spawning→working transition
	log := slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelWarn}))
	mgr := lifecycle.NewManager(store, plugins, log)
	_ = mgr.Advance(ctx, sess)

	// Check what each agent produced.
	fmt.Println("\n=== Results ===")
	hasCommits := false
	for role, rec := range sess.Agents {
		var output string
		exitCode := rec.ExitCode
		if prt, ok := rt.(*processRuntime); ok {
			output = prt.GetOutput(rec.RuntimeHandleID)
			exitCode = prt.GetExitCode(rec.RuntimeHandleID)
		}

		fmt.Printf("\n--- %s (%s) ---\n", role, rec.Backend)
		fmt.Printf("Exit code: %d\n", exitCode)

		// Check for git commits in the worktree
		if rec.WorktreePath != "" {
			commits, gerr := runGitInDir(
				ctx, rec.WorktreePath,
				"log", "--oneline", base+"..HEAD",
			)
			if gerr == nil && commits != "" {
				hasCommits = true
				fmt.Printf("Commits:\n%s\n", commits)
			} else {
				fmt.Println("No commits")
			}
		}
		fmt.Printf("Output: %d bytes\n", len(output))
	}

	// Run manager agent to synthesize worker outputs
	if hasCommits && len(sess.Agents) > 1 {
		workers := make(map[string]workspace.WorkerInfo)
		for role, rec := range sess.Agents {
			var workerOutput string
			if prt, ok := rt.(*processRuntime); ok {
				workerOutput = prt.GetOutput(rec.RuntimeHandleID)
			}
			workers[role] = workspace.WorkerInfo{
				Output:       workerOutput,
				WorktreePath: rec.WorktreePath,
				BaseBranch:   base,
			}
		}

		mgrBackend := "codex"
		for _, rec := range sess.Agents {
			if rec.Backend == "claude" {
				mgrBackend = "claude"
				break
			}
		}

		fmt.Println("\n=== Manager Synthesis ===")
		mgrResult, mgrErr := workspace.RunManager(
			ctx, workspace.ManagerConfig{
				Task:    task,
				Workers: workers,
				GitRoot: gitRoot,
				WorkDir: gitRoot,
				Backend: mgrBackend,
			})
		if mgrErr != nil {
			fmt.Printf("Manager agent failed: %v\n", mgrErr)
		} else {
			fmt.Printf("Summary: %s\n", mgrResult.Summary)
		}
	}

	// Merge agent worktree changes into a single branch
	if hasCommits {
		mergeBranch, mergeErr := workspace.MergeAgentWork(
			ctx, gitRoot, base, sess.ID[:8], sess.Agents)
		if mergeErr != nil {
			fmt.Printf(
				"\nMerge failed: %v\n", mergeErr)
			fmt.Println(
				"Agent worktrees preserved for manual merge.")
		} else {
			fmt.Printf(
				"\nMerged to branch: %s\n", mergeBranch)

			if !noPR {
				fmt.Println(
					"Create PR with: " +
						"gh pr create --head " + mergeBranch)
			}
		}
	}

	// Persist final state
	now := time.Now()
	sess.CompletedAt = &now
	sess.Status = workspace.WSSDone
	if err := store.SaveSession(sess); err != nil {
		return fmt.Errorf("save final: %w", err)
	}

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
		if jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("No workspaces found.")
		}
		return nil
	}

	var sessions []*workspace.WorkspaceSession
	for _, id := range ids {
		sess, lerr := store.LoadSession(id)
		if lerr != nil {
			continue
		}
		// A workspace whose UpdatedAt is more than 6 hours old in a
		// non-terminal state was almost certainly killed (process
		// crash, machine reboot, host shutdown). Without this guard
		// the listing showed "spawning" indefinitely and users
		// thought the agent was still working.
		const staleThreshold = 6 * time.Hour
		if !isTerminalStatus(sess.Status) {
			lastSeen := sess.UpdatedAt
			if lastSeen.IsZero() {
				lastSeen = sess.CreatedAt
			}
			if time.Since(lastSeen) > staleThreshold {
				sess.Status = workspace.WSSFailed
				if sess.Error == "" {
					sess.Error = fmt.Sprintf("stale: no updates for %s", time.Since(lastSeen).Truncate(time.Hour))
				}
			}
		}
		if !showAll && isTerminalStatus(sess.Status) {
			continue
		}
		sessions = append(sessions, sess)
	}

	if jsonOut {
		if len(sessions) == 0 {
			fmt.Println("[]")
			return nil
		}
		data, _ := json.Marshal(sessions)
		fmt.Println(string(data))
		return nil
	}

	for _, sess := range sessions {
		ago := time.Since(sess.CreatedAt).Truncate(time.Second)
		agents := agentSummary(sess)
		fmt.Printf("%-12s %-15s %-30s %8s  %s\n",
			truncateID(sess.ID), sess.Status,
			truncateTask(sess.Task, 30), ago, agents)
	}
	return nil
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
	rt *processRuntime,
	gitRoot string,
) workspace.PluginSet {
	agents := make(map[string]workspace.Agent)
	for _, d := range detected {
		agent, err := backends.NewBackend(d.Name)
		if err == nil {
			agents[d.Name] = agent
		}
	}
	// SCM: try GitHub (requires gh CLI), fall back to noop
	var scmPlugin workspace.SCM
	ghSCM, err := scm.NewGitHubSCM()
	if err == nil {
		scmPlugin = ghSCM
	} else {
		scmPlugin = &scm.NoopSCM{}
	}
	return workspace.PluginSet{
		Runtime:   rt,
		Agents:    agents,
		Workspace: workspace.NewWorktreeWorkspace(),
		SCM:       scmPlugin,
		Notifier:  &noopNotifier{},
	}
}

// buildPluginSetFromRuntime is like buildPluginSet but accepts
// any workspace.Runtime (processRuntime or TmuxRuntime).
func buildPluginSetFromRuntime(
	detected []backends.DetectedBackend,
	rt workspace.Runtime,
	gitRoot string,
) workspace.PluginSet {
	agents := make(map[string]workspace.Agent)
	for _, d := range detected {
		agent, err := backends.NewBackend(d.Name)
		if err == nil {
			agents[d.Name] = agent
		}
	}
	var scmPlugin workspace.SCM
	ghSCM, err := scm.NewGitHubSCM()
	if err == nil {
		scmPlugin = ghSCM
	} else {
		scmPlugin = &scm.NoopSCM{}
	}
	return workspace.PluginSet{
		Runtime:   rt,
		Agents:    agents,
		Workspace: workspace.NewWorktreeWorkspace(),
		SCM:       scmPlugin,
		Notifier:  &noopNotifier{},
	}
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

// noopNotifier satisfies workspace.Notifier with no-op methods.
type noopNotifier struct{}

func (n *noopNotifier) Notify(_ context.Context, _ workspace.NotifyEvent) error {
	return nil
}
func (n *noopNotifier) Name() string { return "none" }
