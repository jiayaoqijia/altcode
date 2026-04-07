package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// WorktreeWorkspace implements the Workspace interface using git worktrees.
type WorktreeWorkspace struct{}

// NewWorktreeWorkspace returns a new WorktreeWorkspace.
func NewWorktreeWorkspace() *WorktreeWorkspace {
	return &WorktreeWorkspace{}
}

// Name returns the workspace type identifier.
func (w *WorktreeWorkspace) Name() string { return "worktree" }

// Setup creates a git worktree with a new branch, symlinks deps,
// and creates the context.md symlink for shared agent notes.
func (w *WorktreeWorkspace) Setup(
	ctx context.Context,
	req WorkspaceSetupRequest,
) (*WorkspaceSetupResult, error) {
	branch := req.Branch
	err := runGitWorktreeAdd(ctx, req.GitRoot, req.WorktreePath, branch, req.BaseRef)
	if err != nil && isBranchExists(err) {
		branch = fmt.Sprintf("%s-%d", req.Branch, time.Now().Unix())
		err = runGitWorktreeAdd(ctx, req.GitRoot, req.WorktreePath, branch, req.BaseRef)
	}
	if err != nil {
		return nil, fmt.Errorf("worktree setup: %w", err)
	}

	baseCommit, _ := runGit(ctx, req.WorktreePath, "rev-parse", "HEAD")

	if err := symlinkDeps(req.GitRoot, req.WorktreePath, req.SymlinkDeps); err != nil {
		return nil, fmt.Errorf("symlink deps: %w", err)
	}

	return &WorkspaceSetupResult{
		Path:       req.WorktreePath,
		Branch:     branch,
		BaseCommit: strings.TrimSpace(baseCommit),
	}, nil
}

// Teardown removes the worktree. Best-effort: ignores errors if
// the worktree was already removed.
func (w *WorktreeWorkspace) Teardown(ctx context.Context, path string) error {
	gitRoot, err := findGitRoot(ctx, path)
	if err != nil {
		return os.RemoveAll(path)
	}
	_, rerr := runGit(ctx, gitRoot, "worktree", "remove", "--force", path)
	if rerr != nil {
		return os.RemoveAll(path)
	}
	return nil
}

// Checkpoint stages all changes and commits them. Sets
// ALTCODE_CHECKPOINT_ACTIVE=1 as a recursion guard for PATH wrappers.
func (w *WorktreeWorkspace) Checkpoint(
	ctx context.Context,
	path string,
	msg string,
) (string, error) {
	if _, err := runGitWithEnv(
		ctx, path,
		[]string{"ALTCODE_CHECKPOINT_ACTIVE=1"},
		"add", "-A",
	); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	_, err := runGitWithEnv(
		ctx, path,
		[]string{"ALTCODE_CHECKPOINT_ACTIVE=1"},
		"commit", "--allow-empty", "-m", msg,
	)
	if err != nil {
		// "nothing to commit" still exits non-zero on some git versions.
		if strings.Contains(err.Error(), "nothing to commit") {
			hash, herr := runGit(ctx, path, "rev-parse", "HEAD")
			if herr != nil {
				return "", herr
			}
			return strings.TrimSpace(hash), nil
		}
		return "", fmt.Errorf("git commit: %w", err)
	}
	hash, err := runGit(ctx, path, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(hash), nil
}

// BranchName builds a deterministic branch name from workspace metadata.
// Format: altcode/{workspaceID}/{role}/{slug}
// slug: first 30 chars, lowered, non-alphanum/-/_ replaced with '-'.
func BranchName(workspaceID, role, taskSlug string) string {
	slug := sanitizeSlug(taskSlug, 30)
	return fmt.Sprintf("altcode/%s/%s/%s", workspaceID, role, slug)
}

// SymlinkContextMD creates a symlink from the workspace's context.md
// into the worktree at .altcode-context.md so agents can read/write it.
func SymlinkContextMD(workspaceDir, worktreePath string) error {
	src := filepath.Join(workspaceDir, "context.md")
	dst := filepath.Join(worktreePath, ".altcode-context.md")
	// Ensure source exists (create empty if not).
	if _, err := os.Stat(src); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(src, nil, 0o644); err != nil {
			return err
		}
	}
	os.Remove(dst) // remove stale symlink
	return os.Symlink(src, dst)
}

// --- helpers ---

var slugRe = regexp.MustCompile(`[^a-z0-9-]`)

func sanitizeSlug(s string, maxLen int) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = slugRe.ReplaceAllString(s, "-")
	// Collapse repeated dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	s = strings.TrimRight(s, "-")
	return s
}

func runGitWorktreeAdd(
	ctx context.Context,
	gitRoot, wtPath, branch, baseRef string,
) error {
	// git worktree add --detach <path> <baseRef>
	if _, err := runGit(ctx, gitRoot, "worktree", "add", "--detach", wtPath, baseRef); err != nil {
		return err
	}
	// Create branch inside the worktree.
	if _, err := runGit(ctx, wtPath, "checkout", "-b", branch); err != nil {
		// Clean up the detached worktree on branch creation failure.
		removeWorktree(ctx, gitRoot, wtPath)
		return err
	}
	return nil
}

// removeWorktree removes a worktree, best-effort.
func removeWorktree(ctx context.Context, gitRoot, wtPath string) {
	_, _ = runGit(ctx, gitRoot, "worktree", "remove", "--force", wtPath)
	os.RemoveAll(wtPath)
}

func isBranchExists(err error) bool {
	return strings.Contains(err.Error(), "already exists")
}

func findGitRoot(ctx context.Context, path string) (string, error) {
	out, err := runGit(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// symlinkDeps symlinks dependency directories from the git root into
// the worktree, avoiding multi-GB copies.
func symlinkDeps(gitRoot, worktreePath string, deps []string) error {
	for _, dep := range deps {
		src := filepath.Join(gitRoot, dep)
		dst := filepath.Join(worktreePath, dep)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		os.Remove(dst) // remove if already exists
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s: %w", dep, err)
		}
	}
	return nil
}

// runGit executes a git command in the given directory and returns stdout.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitWithEnv(ctx, dir, nil, args...)
}

// runGitWithEnv executes a git command with extra env vars.
func runGitWithEnv(
	ctx context.Context,
	dir string,
	extraEnv []string,
	args ...string,
) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		combined := strings.TrimSpace(stderr.String() + " " + stdout.String())
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), combined, err)
	}
	return stdout.String(), nil
}
