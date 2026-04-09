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
func MergeAgentWork(
	ctx context.Context,
	gitRoot, baseBranch, shortID string,
	agents map[string]*AgentRecord,
) (string, error) {
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
		ctx, gitRoot, baseBranch, agents)

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
func cherryPickAgents(
	ctx context.Context,
	gitRoot, baseBranch string,
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
			// Surface conflict on the agent record for TUI visibility
			// ActivityBlocked triggers AttentionRed in the TUI pane
			rec.ActivityState = ActivityBlocked
			continue
		}
		merged++
	}
	return merged
}
