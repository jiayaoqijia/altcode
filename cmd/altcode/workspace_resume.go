package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/workspace"
	"github.com/altcode-ai/altcode/internal/workspace/backends"
	"github.com/spf13/cobra"
)

// --- altcode workspace resume [id] ---

func workspaceResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume [id]",
		Short: "Re-attach to live agents and re-spawn dead ones",
		Long: `Resume a saved workspace by re-attaching to live agent
processes and re-spawning any that have died. Uses PIDs
persisted at spawn time to check liveness via signal 0.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkspaceResume(args)
		},
	}
}

// runWorkspaceResume implements true session resume:
// 1. Load session + PIDs from disk
// 2. Check each agent PID alive via signal 0
// 3. Re-register streaming callbacks for alive agents
// 4. Re-spawn dead agents with --resume where supported
// 5. Start the wait loop
func runWorkspaceResume(args []string) error {
	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt)
	defer stop()

	wd, _ := os.Getwd()
	root := config.DetectProjectRoot(wd)
	wsDir := filepath.Join(root, ".altcode", "workspace")
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

	// Load persisted PIDs
	pids, pidErr := loadPIDs(wsDir, sess.ID)
	if pidErr != nil {
		fmt.Printf("No PID data found, agents cannot "+
			"be re-attached: %v\n", pidErr)
		pids = make(map[string]pidInfo)
	}

	// Detect available backends for re-spawn
	available, err := backends.DetectBackends(ctx)
	if err != nil {
		return fmt.Errorf("detect backends: %w", err)
	}

	rt := &processRuntime{}
	defer rt.KillAll()
	plugins := buildPluginSet(available, rt, sess.GitRoot)

	alive, dead, finished := classifyAgents(
		ctx, sess, pids, rt)

	fmt.Printf("Workspace %s resume:\n", sess.ID)
	fmt.Printf("  Alive:    %v\n", alive)
	fmt.Printf("  Finished: %v\n", finished)
	fmt.Printf("  Dead:     %v\n", dead)

	// Re-spawn dead agents
	respawnDead(ctx, sess, dead, wsDir, plugins, rt)

	sess.Status = workspace.WSSWorking
	if err := st.SaveSession(sess); err != nil {
		return fmt.Errorf("save: %w", err)
	}

	// Update PIDs file with re-spawned agents
	if err := savePIDs(wsDir, sess.ID, sess.Agents); err != nil {
		slog.Warn("failed to save PIDs on resume",
			"err", err)
	}

	// If nothing is running, we're done
	if len(alive) == 0 && len(dead) == 0 {
		fmt.Println("All agents finished. Nothing to wait for.")
		return nil
	}

	return resumeWaitLoop(ctx, sess, rt, st)
}

// classifyAgents sorts agents into alive/dead/finished buckets.
// Alive agents get re-registered with the runtime for streaming.
func classifyAgents(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	pids map[string]pidInfo,
	rt *processRuntime,
) (alive, dead, finished []string) {
	for role, rec := range sess.Agents {
		pi, hasPID := pids[role]
		if hasPID && isPIDAlive(pi.PID) {
			reattachAlive(rt, rec, pi, role)
			alive = append(alive, role)
			continue
		}
		if hasWorktreeCommits(ctx, rec, sess.BaseBranch) {
			finished = append(finished, role)
			rec.ActivityState = workspace.ActivityExited
			continue
		}
		dead = append(dead, role)
	}
	return alive, dead, finished
}

// reattachAlive re-registers a live agent with the runtime.
func reattachAlive(
	rt *processRuntime,
	rec *workspace.AgentRecord,
	pi pidInfo,
	role string,
) {
	handleID := pi.HandleID
	rec.RuntimeHandleID = handleID
	rt.mu.Lock()
	if rt.procs == nil {
		rt.procs = make(map[string]*os.Process)
	}
	if proc, err := os.FindProcess(pi.PID); err == nil {
		rt.procs[handleID] = proc
	}
	rt.mu.Unlock()
	r := role
	rt.OnOutput(handleID, func(line string) {
		fmt.Printf("[%s] %s\n", r, line)
	})
}

// hasWorktreeCommits checks if an agent's worktree has
// commits beyond the base branch.
func hasWorktreeCommits(
	ctx context.Context,
	rec *workspace.AgentRecord,
	baseBranch string,
) bool {
	if rec.WorktreePath == "" {
		return false
	}
	commits, err := runGitInDir(
		ctx, rec.WorktreePath,
		"log", "--oneline", baseBranch+"..HEAD",
	)
	return err == nil && commits != ""
}

// respawnDead re-launches dead agents, preferring --resume
// when the backend supports it.
func respawnDead(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	dead []string,
	wsDir string,
	plugins workspace.PluginSet,
	rt *processRuntime,
) {
	for _, role := range dead {
		rec := sess.Agents[role]
		agent, ok := plugins.Agents[rec.Backend]
		if !ok {
			fmt.Printf("  [%s] backend %q not available, "+
				"skipping\n", role, rec.Backend)
			continue
		}

		as := &workspace.AgentSession{
			WorkspacePath: filepath.Join(wsDir, sess.ID),
			WorktreePath:  rec.WorktreePath,
			Branch:        rec.Branch,
			Task:          sess.Task,
			Role:          rec.Role,
			Model:         rec.Model,
			MaxTurns:      50,
			Env:           os.Environ(),
			AOSessionID:   sess.ID,
		}
		if rec.SessionID != "" {
			as.PriorSessionID = rec.SessionID
		}

		extra, _ := agent.Environment(as)
		for k, v := range extra {
			as.Env = append(as.Env, k+"="+v)
		}

		argv := resolveRestoreArgv(agent, as)
		if len(argv) == 0 {
			fmt.Printf("  [%s] no launch cmd, skipping\n",
				role)
			continue
		}

		handle, err := plugins.Runtime.Spawn(
			ctx, argv, as.Env, rec.WorktreePath)
		if err != nil {
			fmt.Printf("  [%s] spawn failed: %v\n",
				role, err)
			continue
		}
		rec.RuntimeHandleID = handle.ID
		rec.SpawnedAt = handle.StartedAt
		rec.ActivityState = workspace.ActivitySpawning
		rec.RestartCount++

		r := role
		rt.OnOutput(handle.ID, func(line string) {
			fmt.Printf("[%s] %s\n", r, line)
		})
		fmt.Printf("  [%s] re-spawned (%s)\n",
			role, handle.ID)
	}
}

// resolveRestoreArgv tries RestoreCommand first, falls back
// to LaunchCommand.
func resolveRestoreArgv(
	agent workspace.Agent, as *workspace.AgentSession,
) []string {
	if as.PriorSessionID != "" {
		if argv, err := agent.RestoreCommand(as); err == nil && len(argv) > 0 {
			return argv
		}
	}
	argv, err := agent.LaunchCommand(as)
	if err != nil {
		return nil
	}
	return argv
}

// resumeWaitLoop polls until all agents exit or timeout.
func resumeWaitLoop(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	rt *processRuntime,
	st *workspace.Store,
) error {
	fmt.Println("Waiting for agents to complete...")
	deadline := time.After(10 * time.Minute)
	pollTick := time.NewTicker(2 * time.Second)
	defer pollTick.Stop()

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
			return nil
		case <-ctx.Done():
			rt.KillAll()
			return ctx.Err()
		}
	}

	// Update exit info
	for _, rec := range sess.Agents {
		exitCode := rt.GetExitCode(rec.RuntimeHandleID)
		if exitCode >= 0 {
			rec.ExitCode = exitCode
			rec.ActivityState = workspace.ActivityExited
			now := time.Now()
			rec.ExitedAt = &now
		}
	}

	now := time.Now()
	sess.CompletedAt = &now
	sess.Status = workspace.WSSDone
	if err := st.SaveSession(sess); err != nil {
		return fmt.Errorf("save final: %w", err)
	}

	fmt.Printf("Workspace resume complete\n")
	return nil
}

// --- PID persistence ---

// pidInfo records a spawned agent process for resume.
type pidInfo struct {
	PID       int       `json:"pid"`
	Role      string    `json:"role"`
	HandleID  string    `json:"handle_id"`
	StartedAt time.Time `json:"started_at"`
}

// savePIDs writes agent PID info to {wsDir}/{id}/pids.json.
func savePIDs(
	wsDir, id string,
	agents map[string]*workspace.AgentRecord,
) error {
	pids := make(map[string]pidInfo)
	for role, rec := range agents {
		var pid int
		fmt.Sscanf(rec.RuntimeHandleID, "pid:%d", &pid)
		if pid == 0 {
			continue
		}
		pids[role] = pidInfo{
			PID:       pid,
			Role:      role,
			HandleID:  rec.RuntimeHandleID,
			StartedAt: rec.SpawnedAt,
		}
	}
	dir := filepath.Join(wsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir pids: %w", err)
	}
	data, err := json.MarshalIndent(pids, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pids: %w", err)
	}
	p := filepath.Join(dir, "pids.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write pids tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename pids: %w", err)
	}
	return nil
}

// loadPIDs reads agent PID info from {wsDir}/{id}/pids.json.
func loadPIDs(
	wsDir, id string,
) (map[string]pidInfo, error) {
	p := filepath.Join(wsDir, id, "pids.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read pids: %w", err)
	}
	var pids map[string]pidInfo
	if err := json.Unmarshal(data, &pids); err != nil {
		return nil, fmt.Errorf("unmarshal pids: %w", err)
	}
	return pids, nil
}

// isPIDAlive checks whether a process is still running via signal 0.
func isPIDAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
