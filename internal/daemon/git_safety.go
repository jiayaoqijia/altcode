package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// ErrMergeConflict is returned when a rebase detects conflicts.
var ErrMergeConflict = errors.New("merge conflict detected")

// GitOps provides git operations for the daemon's FINALIZE phase.
type GitOps struct {
	workDir string
	logger  *slog.Logger
	run     cmdRunner
}

// NewGitOps creates a GitOps targeting the given working directory.
func NewGitOps(workDir string, logger *slog.Logger) *GitOps {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &GitOps{
		workDir: workDir,
		logger:  logger,
		run:     defaultCmdRunner,
	}
}

// RebaseOntoTarget rebases the current branch onto the target
// (usually the default branch). Returns nil if clean,
// ErrMergeConflict if conflicts are detected (rebase is aborted).
func (g *GitOps) RebaseOntoTarget(
	ctx context.Context, targetBranch string,
) error {
	// Fetch the latest target.
	out, err := g.run(
		ctx, g.workDir, "git", "fetch", "origin", targetBranch,
	)
	if err != nil {
		return fmt.Errorf(
			"git fetch: %w: %s", err, string(out),
		)
	}

	// Attempt rebase.
	out, err = g.run(
		ctx, g.workDir,
		"git", "rebase", "origin/"+targetBranch,
	)
	if err != nil {
		stderr := string(out)
		if strings.Contains(stderr, "CONFLICT") ||
			strings.Contains(stderr, "conflict") {
			// Abort the partial rebase.
			abortOut, abortErr := g.run(
				ctx, g.workDir, "git", "rebase", "--abort",
			)
			if abortErr != nil {
				g.logger.Warn("rebase abort failed",
					"err", abortErr,
					"out", string(abortOut))
			}
			return ErrMergeConflict
		}
		return fmt.Errorf("git rebase: %w: %s", err, stderr)
	}
	return nil
}

// CheckBranchProtection queries GitHub API for branch protection
// rules. Returns a list of warnings (informational, not blocking).
func (g *GitOps) CheckBranchProtection(
	ctx context.Context,
	ghClient *GitHubClient,
	branch string,
) []string {
	endpoint := fmt.Sprintf(
		"repos/%s/branches/%s/protection",
		ghClient.repo(), branch,
	)
	out, err := ghClient.run(
		ctx, g.workDir, "gh", "api", endpoint,
	)
	if err != nil {
		// 404 means no protection — not an error.
		if strings.Contains(string(out), "Not Found") ||
			strings.Contains(string(out), "404") {
			return nil
		}
		g.logger.Warn("branch protection check failed",
			"branch", branch, "err", err)
		return nil
	}

	var warnings []string
	body := string(out)

	if strings.Contains(body, "required_status_checks") {
		warnings = append(warnings,
			"branch has required status checks")
	}
	if strings.Contains(body, "required_pull_request_reviews") {
		warnings = append(warnings,
			"branch requires PR reviews before merge")
	}
	if strings.Contains(body, "enforce_admins") &&
		strings.Contains(body, `"enabled":true`) {
		warnings = append(warnings,
			"branch protection enforced for admins")
	}
	return warnings
}

// CommitWithHookRetry attempts git commit, retrying on pre-commit
// hook failure. After maxRetries, commits with --no-verify and
// returns bypassed=true.
func (g *GitOps) CommitWithHookRetry(
	ctx context.Context, message string, maxRetries int,
) (bypassed bool, err error) {
	if maxRetries <= 0 {
		maxRetries = 1
	}
	for i := 0; i < maxRetries; i++ {
		err = g.gitCommit(ctx, message)
		if err == nil {
			return false, nil
		}
		if !isHookFailure(err) {
			return false, err
		}
		g.logger.Warn("pre-commit hook failed, retrying",
			"attempt", i+1, "max", maxRetries)
	}
	// Exhausted retries: commit with --no-verify.
	err = g.gitCommitNoVerify(ctx, message)
	return true, err
}

// GetDefaultBranch detects the default branch via git remote.
func (g *GitOps) GetDefaultBranch(
	ctx context.Context,
) (string, error) {
	out, err := g.run(
		ctx, g.workDir,
		"git", "symbolic-ref", "refs/remotes/origin/HEAD",
	)
	if err != nil {
		return "", fmt.Errorf(
			"git symbolic-ref: %w: %s", err, string(out),
		)
	}
	// Output: "refs/remotes/origin/main\n"
	ref := strings.TrimSpace(string(out))
	parts := strings.Split(ref, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf(
			"unexpected symbolic-ref output: %q", ref,
		)
	}
	return parts[len(parts)-1], nil
}

func (g *GitOps) gitCommit(
	ctx context.Context, message string,
) error {
	out, err := g.run(
		ctx, g.workDir, "git", "commit", "-m", message,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", string(out), err)
	}
	return nil
}

func (g *GitOps) gitCommitNoVerify(
	ctx context.Context, message string,
) error {
	out, err := g.run(
		ctx, g.workDir,
		"git", "commit", "--no-verify", "-m", message,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", string(out), err)
	}
	return nil
}

// isHookFailure checks if a git commit error was caused by a
// pre-commit hook.
func isHookFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "hook") ||
		strings.Contains(msg, "pre-commit") ||
		strings.Contains(msg, "husky")
}
