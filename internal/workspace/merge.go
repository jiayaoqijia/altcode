package workspace

import (
	"context"
	"fmt"
	"strings"
)

// MergeAgentWork combines all agent worktree changes into one branch.
// For each agent: commit uncommitted changes, then cherry-pick onto
// a new merge branch. Agents with conflicts are skipped with a
// warning printed to stdout.
//
// Takes a *WorkspaceSession (not just the agent map) so it can hold
// sess.mu when mutating rec.ActivityState on conflict. The previous
// code wrote rec.ActivityState = ActivityBlocked without any lock,
// racing with SaveSession reading the same field under sess.mu.
func MergeAgentWork(
	ctx context.Context,
	gitRoot, baseBranch, shortID string,
	sess *WorkspaceSession,
) (string, error) {
	// Snapshot the agent map under sess.mu so we can iterate without
	// concurrent /spawn or lifecycle goroutines mutating it under us.
	sess.Lock()
	agents := make(map[string]*AgentRecord, len(sess.Agents))
	for k, v := range sess.Agents {
		agents[k] = v
	}
	sess.Unlock()

	commitUnstagedWork(ctx, agents)

	mergeBranch := fmt.Sprintf(
		"altcode/merge/%s", shortID)
	_, err := runGit(ctx, gitRoot,
		"checkout", "-b", mergeBranch, baseBranch)
	if err != nil {
		return "", fmt.Errorf(
			"create merge branch: %w", err)
	}

	merged := cherryPickAgents(
		ctx, gitRoot, baseBranch, sess, agents)

	if merged == 0 {
		runGit(ctx, gitRoot, //nolint:errcheck
			"checkout", baseBranch)
		runGit(ctx, gitRoot, //nolint:errcheck
			"branch", "-D", mergeBranch)
		return "", fmt.Errorf(
			"no agent commits could be merged")
	}

	// Return to base branch, leave merge branch intact
	runGit(ctx, gitRoot, "checkout", baseBranch) //nolint:errcheck

	return mergeBranch, nil
}

// commitUnstagedWork auto-commits any dirty worktrees.
func commitUnstagedWork(
	ctx context.Context,
	agents map[string]*AgentRecord,
) {
	for role, rec := range agents {
		if rec.WorktreePath == "" {
			continue
		}
		status, _ := runGit(
			ctx, rec.WorktreePath,
			"status", "--porcelain")
		if strings.TrimSpace(status) == "" {
			continue
		}
		runGit(ctx, rec.WorktreePath, //nolint:errcheck
			"add", "-A")
		runGit(ctx, rec.WorktreePath, //nolint:errcheck
			"commit", "-m", "altcode: "+role+" work")
	}
}

// cherryPickAgents cherry-picks each agent's new commits onto
// the current branch (the merge branch). Returns the count of
// agents whose commits were successfully picked.
//
// Conflict marking takes sess.mu so the rec.ActivityState write
// doesn't race with SaveSession or other readers under the same
// lock. The agents map is the snapshot from MergeAgentWork — we
// look up the live record in sess.Agents under the lock before
// writing.
func cherryPickAgents(
	ctx context.Context,
	gitRoot, baseBranch string,
	sess *WorkspaceSession,
	agents map[string]*AgentRecord,
) int {
	merged := 0
	for role, rec := range agents {
		if rec.WorktreePath == "" {
			continue
		}
		headRaw, err := runGit(
			ctx, rec.WorktreePath,
			"rev-parse", "HEAD")
		if err != nil {
			continue
		}
		baseRaw, err := runGit(
			ctx, rec.WorktreePath,
			"merge-base", baseBranch, "HEAD")
		if err != nil {
			continue
		}
		head := strings.TrimSpace(headRaw)
		base := strings.TrimSpace(baseRaw)
		if head == base {
			continue
		}

		_, cerr := runGit(ctx, gitRoot,
			"cherry-pick", base+".."+head)
		if cerr != nil {
			fmt.Printf(
				"  warning: cherry-pick conflict for %s, "+
					"skipping\n", role)
			runGit(ctx, gitRoot, //nolint:errcheck
				"cherry-pick", "--abort")
			// Surface conflict on the agent record for TUI visibility.
			// Take sess.mu so the write doesn't race with SaveSession
			// reading sess.Agents[role].ActivityState under the same
			// lock — this used to be the unlocked write the round 8
			// race scan caught.
			sess.Lock()
			if liveRec, ok := sess.Agents[role]; ok && liveRec != nil {
				liveRec.ActivityState = ActivityBlocked
			}
			sess.Unlock()
			continue
		}
		merged++
	}
	return merged
}
