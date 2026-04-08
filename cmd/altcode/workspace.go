package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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

	// Create worktrees and inject context.
	// Track created worktrees for cleanup on partial failure.
	wksp := workspace.NewWorktreeWorkspace()
	home, _ := os.UserHomeDir()
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

	// Spawn agents
	rt := &processRuntime{}
	defer rt.KillAll() // ensure cleanup on exit/Ctrl+C
	plugins := buildPluginSet(selected, rt, gitRoot)
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

		// Stream agent output to stdout with role prefix
		role := rec.Role
		rt.OnOutput(handle.ID, func(line string) {
			fmt.Printf("[%s] %s\n", role, line)
		})
	}

	if err := store.SaveSession(sess); err != nil {
		return fmt.Errorf("save session after spawn: %w", err)
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
			if rt.IsStillRunning(rec.RuntimeHandleID) {
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

	// Update agent records with exit info
	for _, rec := range sess.Agents {
		exitCode := rt.GetExitCode(rec.RuntimeHandleID)
		if exitCode >= 0 {
			rec.ExitCode = exitCode
			rec.ActivityState = workspace.ActivityExited
			now := time.Now()
			rec.ExitedAt = &now
		}
	}
	sess.Status = workspace.WSSWorking

	// Run one lifecycle advance to handle the spawning→working transition
	log := slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelWarn}))
	mgr := lifecycle.NewManager(store, plugins, log)
	_ = mgr.Advance(ctx, sess)

	// Check what each agent produced
	fmt.Println("\n=== Results ===")
	hasCommits := false
	for role, rec := range sess.Agents {
		output := rt.GetOutput(rec.RuntimeHandleID)
		exitCode := rt.GetExitCode(rec.RuntimeHandleID)

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
		workerOutputs := make(map[string]string)
		for role, rec := range sess.Agents {
			workerOutputs[role] = rt.GetOutput(
				rec.RuntimeHandleID)
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
				Task:          task,
				WorkerOutputs: workerOutputs,
				GitRoot:       gitRoot,
				WorkDir:       gitRoot,
				Backend:       mgrBackend,
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

// --- altcode workspace resume [id] ---

func workspaceResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume [id]",
		Short: "Resume a saved workspace (most recent if no ID given)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd, _ := os.Getwd()
			root := config.DetectProjectRoot(wd)
			wsDir := filepath.Join(
				root, ".altcode", "workspace")
			st := workspace.NewStore(wsDir)

			id, err := resolveWorkspaceID(st, args)
			if err != nil {
				return err
			}
			sess, err := st.LoadSession(id)
			if err != nil {
				return fmt.Errorf("load: %w", err)
			}
			if isTerminalStatus(sess.Status) {
				return fmt.Errorf(
					"workspace %s is %s, cannot resume",
					sess.ID, sess.Status)
			}
			sess.Status = workspace.WSSWorking
			if err := st.SaveSession(sess); err != nil {
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

// processRuntime is a minimal Runtime that spawns OS processes.
// Tracks spawned processes and captures output for one-shot agents.
type processRuntime struct {
	mu        sync.Mutex
	procs     map[string]*os.Process
	outputs   map[string]*bytes.Buffer // captured stdout per agent
	exits     map[string]int           // exit codes per agent
	callbacks map[string]func(string)  // handle ID → line callback
}

func (p *processRuntime) Name() string { return "process" }

// OnOutput registers a per-line callback for a spawned process.
// Lines are delivered in real-time as the process writes to stdout/stderr.
func (p *processRuntime) OnOutput(
	handleID string, cb func(string),
) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.callbacks == nil {
		p.callbacks = make(map[string]func(string))
	}
	p.callbacks[handleID] = cb
}

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

	// Pipe-based streaming: scan lines for callbacks + capture buffer
	var buf bytes.Buffer
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return workspace.RuntimeHandle{},
			fmt.Errorf("start: %w", err)
	}
	h := workspace.RuntimeHandle{
		ID:        fmt.Sprintf("pid:%d", cmd.Process.Pid),
		StartedAt: time.Now(),
	}
	p.mu.Lock()
	if p.procs == nil {
		p.procs = make(map[string]*os.Process)
	}
	if p.outputs == nil {
		p.outputs = make(map[string]*bytes.Buffer)
	}
	if p.exits == nil {
		p.exits = make(map[string]int)
	}
	if p.callbacks == nil {
		p.callbacks = make(map[string]func(string))
	}
	p.procs[h.ID] = cmd.Process
	p.mu.Unlock()

	// Scanner goroutine: reads lines, writes to buffer, calls callback.
	// Signals readerDone when complete so the wait goroutine can safely
	// access the buffer (fixes data race on buf).
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			buf.WriteString(line + "\n")
			p.mu.Lock()
			cb := p.callbacks[h.ID]
			p.mu.Unlock()
			if cb != nil {
				cb(line)
			}
		}
		pr.Close()
	}()

	// Wait goroutine: waits for process exit, closes pipe writer,
	// then waits for reader goroutine to finish before storing buffer.
	go func() {
		werr := cmd.Wait()
		pw.Close()          // signals EOF to scanner
		<-readerDone        // wait for scanner to finish (no race on buf)
		p.mu.Lock()
		delete(p.procs, h.ID)
		p.outputs[h.ID] = &buf
		if werr != nil {
			if exitErr, ok := werr.(*exec.ExitError); ok {
				p.exits[h.ID] = exitErr.ExitCode()
			} else {
				p.exits[h.ID] = 1
			}
		} else {
			p.exits[h.ID] = 0
		}
		p.mu.Unlock()
	}()
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
	p.mu.Lock()
	proc, ok := p.procs[h.ID]
	p.mu.Unlock()
	if !ok {
		return nil // already exited
	}
	return proc.Kill()
}

// KillAll terminates all tracked agent processes. Called on shutdown.
func (p *processRuntime) KillAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, proc := range p.procs {
		proc.Kill() //nolint:errcheck
	}
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

// IsStillRunning checks a handle ID directly.
func (p *processRuntime) IsStillRunning(
	handleID string,
) bool {
	alive, _ := p.IsRunning(
		workspace.RuntimeHandle{ID: handleID})
	return alive
}

// GetOutput returns captured stdout/stderr for a completed agent.
func (p *processRuntime) GetOutput(
	handleID string,
) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if buf, ok := p.outputs[handleID]; ok {
		return buf.String()
	}
	return ""
}

// GetExitCode returns the exit code for a completed agent.
// Returns -1 if the agent hasn't exited yet.
func (p *processRuntime) GetExitCode(
	handleID string,
) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if code, ok := p.exits[handleID]; ok {
		return code
	}
	return -1
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
