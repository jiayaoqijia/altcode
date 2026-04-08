package lifecycle

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// advanceSpawning transitions to working once all agents are alive,
// or to failed if any agent fails to start within SpawnTimeout.
func advanceSpawning(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) error {
	for _, rec := range sess.Agents {
		alive, err := plugins.Runtime.IsRunning(
			workspace.RuntimeHandle{ID: rec.RuntimeHandleID},
		)
		if err != nil {
			return fmt.Errorf("check alive %s: %w", rec.Role, err)
		}
		if !alive && time.Since(rec.SpawnedAt) > SpawnTimeout {
			sess.Status = workspace.WSSFailed
			sess.Error = fmt.Sprintf(
				"agent %s failed to start within %s", rec.Role, SpawnTimeout)
			return nil
		}
		if !alive {
			return nil // still waiting
		}
	}
	sess.Status = workspace.WSSWorking
	return nil
}

// advanceWorking detects PR creation or SHA changes from working agents.
func advanceWorking(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
	store *workspace.Store,
) error {
	for _, rec := range sess.Agents {
		if err := checkpointOnTurnEnd(ctx, sess, rec, plugins); err != nil {
			return err
		}
		if rec.PRID > 0 && rec.HeadSHA != rec.LastCheckedSHA {
			sess.Status = workspace.WSSCIChecking
			rec.LastCheckedSHA = rec.HeadSHA
			return nil
		}
		if rec.PRID > 0 {
			sess.Status = workspace.WSSPROpen
			return nil
		}
	}
	return nil
}

// advancePROpen waits for CI to start running on the PR.
func advancePROpen(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) error {
	for _, rec := range sess.Agents {
		if rec.PRID == 0 {
			continue
		}
		pr, err := plugins.SCM.GetPR(ctx, rec.PRID)
		if err != nil {
			return fmt.Errorf("get PR %d: %w", rec.PRID, err)
		}
		rec.HeadSHA = pr.HeadSHA
		rec.CIStatus = pr.CIStatus
		rec.ReviewStatus = pr.ReviewStatus
		rec.LastCheckedSHA = pr.HeadSHA
		switch pr.CIStatus {
		case workspace.CIRunning, workspace.CIPending:
			sess.Status = workspace.WSSCIChecking
			return nil
		case workspace.CIPass:
			// CI already passed before we polled — skip to review check
			return onCIPass(ctx, sess, rec, plugins)
		case workspace.CIFail:
			// CI already failed before we polled
			sess.Status = workspace.WSSCIFailed
			return nil
		}
	}
	// Stuck timeout: if PR has been open with no CI for too long
	if time.Since(sess.UpdatedAt) > StuckThreshold {
		sess.Status = workspace.WSSFailed
		sess.Error = "pr_open stuck: CI never started within timeout"
	}
	return nil
}

// advanceCIChecking polls CI and routes to pass or fail states.
func advanceCIChecking(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) error {
	for _, rec := range sess.Agents {
		if rec.PRID == 0 {
			continue
		}
		status, err := plugins.SCM.CIStatus(ctx, rec.HeadSHA)
		if err != nil {
			return fmt.Errorf("ci status %s: %w", rec.HeadSHA, err)
		}
		rec.CIStatus = status
		// Mark SHA as checked to prevent infinite re-entry (spec R3-3)
		rec.LastCheckedSHA = rec.HeadSHA
		if status == workspace.CIFail {
			sess.Status = workspace.WSSCIFailed
			return nil
		}
		if status == workspace.CIPass {
			return onCIPass(ctx, sess, rec, plugins)
		}
	}
	return nil // still running
}

// onCIPass handles the pass branch: check review status.
func onCIPass(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	rec *workspace.AgentRecord,
	plugins workspace.PluginSet,
) error {
	pr, err := plugins.SCM.GetPR(ctx, rec.PRID)
	if err != nil {
		return fmt.Errorf("get PR for review: %w", err)
	}
	rec.ReviewStatus = pr.ReviewStatus
	switch pr.ReviewStatus {
	case workspace.ReviewApproved:
		sess.Status = workspace.WSSApproved
	case workspace.ReviewChangesRequested:
		sess.Status = workspace.WSSChangesRequested
	default:
		sess.Status = workspace.WSSWorking
	}
	return nil
}

// advanceCIFailed dispatches a fix or escalates.
func advanceCIFailed(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
	store *workspace.Store,
) error {
	if sess.CIRetries >= sess.MaxCIRetries {
		sess.Status = workspace.WSSFailed
		sess.Error = "CI auto-fix exhausted"
		return nil
	}
	ciLog := fetchCILog(ctx, sess, plugins)
	if err := DispatchCIFix(ctx, sess, ciLog, store); err != nil {
		return err
	}
	sess.Status = workspace.WSSWorking
	return nil
}

// fetchCILog gets the actual CI failure output for the primary agent's HEAD.
// Uses SCM.CILogs to retrieve real test/build errors for the agent to fix.
func fetchCILog(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) string {
	primary := primaryAgent(sess)
	if primary == nil {
		return "(no primary agent)"
	}
	log, err := plugins.SCM.CILogs(ctx, primary.HeadSHA)
	if err != nil || log == "" {
		return fmt.Sprintf("CI failed for commit %s (logs unavailable)", primary.HeadSHA)
	}
	return log
}

// advanceChangesRequested sends review feedback to the agent.
func advanceChangesRequested(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
	store *workspace.Store,
) error {
	primary := primaryAgent(sess)
	if primary == nil || primary.PRID == 0 {
		return nil
	}
	reviews, err := plugins.SCM.GetPRReviews(ctx, primary.PRID)
	if err != nil {
		return fmt.Errorf("get reviews: %w", err)
	}
	context := ExtractReviewContext(reviews, sess.Task)
	if err := store.SendMessage(
		ctx, sess.ID, primary.Role, context,
	); err != nil {
		return err
	}
	sess.Status = workspace.WSSWorking
	return nil
}

// advanceApproved checks mergeable state.
func advanceApproved(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) error {
	for _, rec := range sess.Agents {
		if rec.PRID == 0 {
			continue
		}
		pr, err := plugins.SCM.GetPR(ctx, rec.PRID)
		if err != nil {
			return fmt.Errorf("get PR %d: %w", rec.PRID, err)
		}
		if pr.CIStatus == workspace.CIRunning {
			sess.Status = workspace.WSSCIChecking
			return nil
		}
		if pr.MergeableState == "conflicting" {
			return nil // stay approved, wait for conflict resolution
		}
		if pr.MergeableState == "mergeable" {
			sess.Status = workspace.WSSMergeable
			return nil
		}
	}
	return nil
}

// advanceMergeable merges the PR if auto-merge is enabled.
func advanceMergeable(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) error {
	if !sess.AutoMerge {
		return nil // operator must merge manually
	}
	method := sess.MergeMethod
	if method == "" {
		method = workspace.MergeSquash
	}
	for _, rec := range sess.Agents {
		if rec.PRID == 0 {
			continue
		}
		pr, err := plugins.SCM.GetPR(ctx, rec.PRID)
		if err != nil {
			return fmt.Errorf("get PR %d: %w", rec.PRID, err)
		}
		if pr.State == "merged" {
			continue
		}
		if err := plugins.SCM.MergePR(ctx, rec.PRID, method); err != nil {
			return fmt.Errorf("merge PR %d: %w", rec.PRID, err)
		}
	}
	sess.Status = workspace.WSSMerged
	return nil
}

// advanceMerged transitions immediately to cleanup.
func advanceMerged(sess *workspace.WorkspaceSession) error {
	sess.Status = workspace.WSSCleanup
	return nil
}

// advanceCleanup tears down worktrees and marks done.
func advanceCleanup(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) error {
	for _, rec := range sess.Agents {
		if rec.WorktreePath == "" {
			continue
		}
		if err := plugins.Workspace.Teardown(
			ctx, rec.WorktreePath,
		); err != nil {
			return fmt.Errorf("teardown %s: %w", rec.Role, err)
		}
		rec.WorktreePath = ""
	}
	now := time.Now()
	sess.CompletedAt = &now
	sess.Status = workspace.WSSDone
	return nil
}

// aggregateWorkspaceStatus computes the worst-case status across
// all agents per errata E6 priority ordering.
func aggregateWorkspaceStatus(
	sess *workspace.WorkspaceSession,
) workspace.WorkspaceStatus {
	priority := map[workspace.WorkspaceStatus]int{
		workspace.WSSFailed:           1,
		workspace.WSSCIFailed:         2,
		workspace.WSSChangesRequested: 3,
		workspace.WSSCIChecking:       4,
		workspace.WSSPROpen:           5,
		workspace.WSSApproved:         6,
		workspace.WSSMergeable:        7,
		workspace.WSSMerged:           8,
		workspace.WSSWorking:          9,
	}
	worst := workspace.WSSWorking
	worstPri := priority[worst]
	for _, rec := range sess.Agents {
		s := agentEffectiveStatus(rec)
		p, ok := priority[s]
		if ok && p < worstPri {
			worst = s
			worstPri = p
		}
	}
	return worst
}

// agentEffectiveStatus derives a workspace-level status from one agent.
func agentEffectiveStatus(
	rec *workspace.AgentRecord,
) workspace.WorkspaceStatus {
	if rec.ActivityState == workspace.ActivityExited && rec.ExitCode != 0 {
		return workspace.WSSFailed
	}
	if rec.CIStatus == workspace.CIFail {
		return workspace.WSSCIFailed
	}
	if rec.ReviewStatus == workspace.ReviewChangesRequested {
		return workspace.WSSChangesRequested
	}
	if rec.CIStatus == workspace.CIRunning {
		return workspace.WSSCIChecking
	}
	if rec.PRID > 0 && rec.ReviewStatus == workspace.ReviewApproved {
		return workspace.WSSApproved
	}
	if rec.PRID > 0 {
		return workspace.WSSPROpen
	}
	return workspace.WSSWorking
}

// checkpointOnTurnEnd detects active→ready/idle transitions and
// creates a git checkpoint per errata E5.
func checkpointOnTurnEnd(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	rec *workspace.AgentRecord,
	plugins workspace.PluginSet,
) error {
	agent, ok := plugins.Agents[rec.Backend]
	if !ok {
		return nil
	}
	prev := rec.ActivityState
	det, err := agent.ActivityState(ctx, buildAgentSession(rec, sess))
	if err != nil || det == nil {
		return nil
	}
	rec.ActivityState = det.State
	rec.ActivityUpdatedAt = det.Timestamp
	wasActive := prev == workspace.ActivityActive
	nowResting := det.State == workspace.ActivityReady ||
		det.State == workspace.ActivityIdle
	if !wasActive || !nowResting {
		return nil
	}
	rec.TurnCount++
	msg := fmt.Sprintf(
		"altcode: checkpoint turn-%03d [%s]", rec.TurnCount, rec.Role)
	hash, err := plugins.Workspace.Checkpoint(
		ctx, rec.WorktreePath, msg)
	if err != nil {
		return err
	}
	rec.HeadSHA = hash
	return nil
}

// buildAgentSession creates an AgentSession from a record + session.
func buildAgentSession(
	rec *workspace.AgentRecord,
	sess *workspace.WorkspaceSession,
) *workspace.AgentSession {
	return &workspace.AgentSession{
		WorktreePath:    rec.WorktreePath,
		Branch:          rec.Branch,
		Task:            sess.Task,
		Role:            rec.Role,
		Model:           rec.Model,
		PriorSessionID:  rec.SessionID,
		RuntimeHandleID: rec.RuntimeHandleID,
		AOSessionID:     sess.ID,
		Env:             os.Environ(), // inherit parent env (API keys, PATH)
	}
}
