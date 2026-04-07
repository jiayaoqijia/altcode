package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initBareRepo creates a bare git repo with one commit so that worktrees
// can branch from HEAD. Returns (gitRoot, cleanup).
func initBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()

	must := func(args ...string) {
		t.Helper()
		if _, err := runGit(ctx, dir, args...); err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
	}
	must("init")
	must("config", "user.email", "test@test.com")
	must("config", "user.name", "test")

	// Seed commit so HEAD exists.
	seed := filepath.Join(dir, "README.md")
	if err := os.WriteFile(seed, []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	must("add", "-A")
	must("commit", "-m", "initial")
	return dir
}

func TestWorktreeSetup(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	ws := NewWorktreeWorkspace()

	wtPath := filepath.Join(t.TempDir(), "wt-setup")
	req := WorkspaceSetupRequest{
		GitRoot:      gitRoot,
		WorktreePath: wtPath,
		Branch:       "altcode/test/architect/setup-task",
		BaseRef:      "HEAD",
		SymlinkDeps:  nil,
	}

	res, err := ws.Setup(ctx, req)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Path != wtPath {
		t.Errorf("Path = %q, want %q", res.Path, wtPath)
	}
	if res.Branch != req.Branch {
		t.Errorf("Branch = %q, want %q", res.Branch, req.Branch)
	}
	if res.BaseCommit == "" {
		t.Error("BaseCommit is empty")
	}

	// Verify the branch exists in the worktree.
	out, err := runGit(ctx, wtPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if got := strings.TrimSpace(out); got != req.Branch {
		t.Errorf("HEAD branch = %q, want %q", got, req.Branch)
	}

	// Verify files are accessible (README.md from seed commit).
	if _, err := os.Stat(filepath.Join(wtPath, "README.md")); err != nil {
		t.Errorf("README.md not found in worktree: %v", err)
	}
}

func TestWorktreeSetup_BranchCollision(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	ws := NewWorktreeWorkspace()

	branch := "altcode/test/impl/collision"

	// Create the branch first so the second setup hits collision.
	if _, err := runGit(ctx, gitRoot, "branch", branch); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	wtPath := filepath.Join(t.TempDir(), "wt-collision")
	req := WorkspaceSetupRequest{
		GitRoot:      gitRoot,
		WorktreePath: wtPath,
		Branch:       branch,
		BaseRef:      "HEAD",
	}
	res, err := ws.Setup(ctx, req)
	if err != nil {
		t.Fatalf("Setup with collision: %v", err)
	}
	// Branch should have a timestamp suffix.
	if res.Branch == branch {
		t.Error("expected branch name to differ due to collision")
	}
	if !strings.HasPrefix(res.Branch, branch+"-") {
		t.Errorf("Branch %q should start with %q-", res.Branch, branch)
	}
}

func TestWorktreeTeardown(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	ws := NewWorktreeWorkspace()

	wtPath := filepath.Join(t.TempDir(), "wt-teardown")
	req := WorkspaceSetupRequest{
		GitRoot:      gitRoot,
		WorktreePath: wtPath,
		Branch:       "altcode/test/teardown/task",
		BaseRef:      "HEAD",
	}
	if _, err := ws.Setup(ctx, req); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if err := ws.Teardown(ctx, wtPath); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree path still exists after teardown")
	}
}

func TestWorktreeCheckpoint(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	ws := NewWorktreeWorkspace()

	wtPath := filepath.Join(t.TempDir(), "wt-checkpoint")
	req := WorkspaceSetupRequest{
		GitRoot:      gitRoot,
		WorktreePath: wtPath,
		Branch:       "altcode/test/checkpoint/task",
		BaseRef:      "HEAD",
	}
	if _, err := ws.Setup(ctx, req); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Write a file and checkpoint.
	fp := filepath.Join(wtPath, "new-file.txt")
	if err := os.WriteFile(fp, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash, err := ws.Checkpoint(ctx, wtPath, "altcode: checkpoint turn-001 [test]")
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if len(hash) < 7 {
		t.Errorf("commit hash too short: %q", hash)
	}

	// Verify commit exists.
	out, err := runGit(ctx, wtPath, "log", "--oneline", "-1")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(out, "altcode: checkpoint turn-001") {
		t.Errorf("commit message not found in log: %q", out)
	}
}

func TestWorktreeCheckpoint_RecursionGuard(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	ws := NewWorktreeWorkspace()

	wtPath := filepath.Join(t.TempDir(), "wt-recurse")
	req := WorkspaceSetupRequest{
		GitRoot:      gitRoot,
		WorktreePath: wtPath,
		Branch:       "altcode/test/recurse/task",
		BaseRef:      "HEAD",
	}
	if _, err := ws.Setup(ctx, req); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Worktree .git is a file pointing to the main repo — hooks live there.
	hooksDir := filepath.Join(gitRoot, ".git", "hooks")
	os.MkdirAll(hooksDir, 0o755)
	marker := filepath.Join(wtPath, ".guard-marker")
	hook := "#!/bin/sh\n" +
		"echo \"ALTCODE_CHECKPOINT_ACTIVE=$ALTCODE_CHECKPOINT_ACTIVE\" > " +
		marker + "\n"
	hookPath := filepath.Join(hooksDir, "pre-commit")
	os.WriteFile(hookPath, []byte(hook), 0o755)

	os.WriteFile(filepath.Join(wtPath, "guard.txt"), []byte("x"), 0o644)
	if _, err := ws.Checkpoint(ctx, wtPath, "guard test"); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !strings.Contains(string(data), "ALTCODE_CHECKPOINT_ACTIVE=1") {
		t.Errorf("recursion guard not set; marker = %q", string(data))
	}
}

func TestBranchName(t *testing.T) {
	tests := []struct {
		wsID, role, task string
		want             string
	}{
		{"01hv", "architect", "add auth system", "altcode/01hv/architect/add-auth-system"},
		{"01hv", "implementer", "Fix Bug #42!", "altcode/01hv/implementer/fix-bug-42"},
		{"abc", "reviewer", strings.Repeat("x", 50), "altcode/abc/reviewer/" + strings.Repeat("x", 30)},
		{"id", "impl", "  spaces  ", "altcode/id/impl/spaces"},
	}
	for _, tt := range tests {
		got := BranchName(tt.wsID, tt.role, tt.task)
		if got != tt.want {
			t.Errorf("BranchName(%q,%q,%q) = %q, want %q",
				tt.wsID, tt.role, tt.task, got, tt.want)
		}
	}
}

func TestSymlinkDeps(t *testing.T) {
	gitRoot := t.TempDir()
	wtPath := t.TempDir()

	// Create node_modules in the git root.
	nm := filepath.Join(gitRoot, "node_modules")
	os.MkdirAll(nm, 0o755)
	os.WriteFile(filepath.Join(nm, "pkg.json"), []byte("{}"), 0o644)

	if err := symlinkDeps(gitRoot, wtPath, []string{"node_modules", ".venv"}); err != nil {
		t.Fatalf("symlinkDeps: %v", err)
	}

	link := filepath.Join(wtPath, "node_modules")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("node_modules is not a symlink")
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != nm {
		t.Errorf("symlink target = %q, want %q", target, nm)
	}

	// .venv doesn't exist — should be silently skipped.
	if _, err := os.Lstat(filepath.Join(wtPath, ".venv")); !os.IsNotExist(err) {
		t.Error(".venv should not be symlinked when source doesn't exist")
	}
}

func TestSymlinkContextMD(t *testing.T) {
	wsDir := t.TempDir()
	wtPath := t.TempDir()

	if err := SymlinkContextMD(wsDir, wtPath); err != nil {
		t.Fatalf("SymlinkContextMD: %v", err)
	}

	link := filepath.Join(wtPath, ".altcode-context.md")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error(".altcode-context.md is not a symlink")
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	want := filepath.Join(wsDir, "context.md")
	if target != want {
		t.Errorf("target = %q, want %q", target, want)
	}
}
