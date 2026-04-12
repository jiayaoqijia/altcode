package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

func newGitOps() *GitOps {
	return NewGitOps("/tmp/test-repo", nil)
}

// --- RebaseOntoTarget ---

func TestRebaseOntoTarget_Clean(t *testing.T) {
	g := newGitOps()
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.HasPrefix(key, "git fetch") {
			return []byte(""), nil
		}
		if strings.HasPrefix(key, "git rebase") {
			return []byte("Successfully rebased"), nil
		}
		return nil, fmt.Errorf("unexpected: %s", key)
	}

	err := g.RebaseOntoTarget(context.Background(), "main")
	if err != nil {
		t.Fatalf("RebaseOntoTarget clean: %v", err)
	}
}

func TestRebaseOntoTarget_Conflict(t *testing.T) {
	g := newGitOps()

	var aborted int32
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.HasPrefix(key, "git fetch") {
			return []byte(""), nil
		}
		if strings.Contains(key, "rebase --abort") {
			atomic.AddInt32(&aborted, 1)
			return []byte(""), nil
		}
		if strings.HasPrefix(key, "git rebase") {
			return []byte(
				"CONFLICT (content): Merge conflict in main.go",
			), fmt.Errorf("exit 1")
		}
		return nil, fmt.Errorf("unexpected: %s", key)
	}

	err := g.RebaseOntoTarget(context.Background(), "main")
	if err != ErrMergeConflict {
		t.Fatalf("expected ErrMergeConflict, got: %v", err)
	}
	if atomic.LoadInt32(&aborted) != 1 {
		t.Error("rebase --abort was not called")
	}
}

// --- CommitWithHookRetry ---

func TestCommitWithHookRetry_Success(t *testing.T) {
	g := newGitOps()

	var calls int32
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte(""), nil
	}

	bypassed, err := g.CommitWithHookRetry(
		context.Background(), "feat: add auth", 3,
	)
	if err != nil {
		t.Fatalf("CommitWithHookRetry: %v", err)
	}
	if bypassed {
		t.Error("expected bypassed=false on first success")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("calls = %d, want 1", atomic.LoadInt32(&calls))
	}
}

func TestCommitWithHookRetry_HookFailThenSuccess(t *testing.T) {
	g := newGitOps()

	var calls int32
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// First attempt: hook failure.
			return []byte("pre-commit hook failed"),
				fmt.Errorf("pre-commit hook failed: exit 1")
		}
		// Second attempt: success.
		return []byte(""), nil
	}

	bypassed, err := g.CommitWithHookRetry(
		context.Background(), "feat: add auth", 3,
	)
	if err != nil {
		t.Fatalf("CommitWithHookRetry: %v", err)
	}
	if bypassed {
		t.Error("expected bypassed=false when second try succeeds")
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("calls = %d, want 2", n)
	}
}

func TestCommitWithHookRetry_ExhaustsRetries(t *testing.T) {
	g := newGitOps()

	var commitCalls int32
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		atomic.AddInt32(&commitCalls, 1)
		if strings.Contains(key, "--no-verify") {
			// Final bypass commit succeeds.
			return []byte(""), nil
		}
		return []byte("husky pre-commit failed"),
			fmt.Errorf("husky pre-commit failed: exit 1")
	}

	bypassed, err := g.CommitWithHookRetry(
		context.Background(), "fix: lint", 2,
	)
	if err != nil {
		t.Fatalf("CommitWithHookRetry: %v", err)
	}
	if !bypassed {
		t.Error("expected bypassed=true when retries exhausted")
	}
	// 2 retries + 1 --no-verify = 3 calls.
	if n := atomic.LoadInt32(&commitCalls); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestCommitWithHookRetry_NonHookError(t *testing.T) {
	g := newGitOps()

	var calls int32
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte("nothing to commit"),
			fmt.Errorf("nothing to commit: exit 1")
	}

	_, err := g.CommitWithHookRetry(
		context.Background(), "fix: empty", 3,
	)
	if err == nil {
		t.Fatal("expected error for non-hook failure")
	}
	// Should return immediately, no retry.
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("calls = %d, want 1 (no retry for non-hook)", n)
	}
}

// --- isHookFailure ---

func TestIsHookFailure(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("exit 1"), false},
		{fmt.Errorf("nothing to commit"), false},
		{fmt.Errorf("pre-commit hook failed"), true},
		{fmt.Errorf("hook '/path/lint' failed"), true},
		{fmt.Errorf("husky - pre-commit failed"), true},
	}
	for _, tt := range tests {
		got := isHookFailure(tt.err)
		if got != tt.want {
			t.Errorf(
				"isHookFailure(%v) = %v, want %v",
				tt.err, got, tt.want,
			)
		}
	}
}

// --- GetDefaultBranch ---

func TestGetDefaultBranch(t *testing.T) {
	g := newGitOps()
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		return []byte("refs/remotes/origin/main\n"), nil
	}

	branch, err := g.GetDefaultBranch(context.Background())
	if err != nil {
		t.Fatalf("GetDefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want %q", branch, "main")
	}
}

func TestGetDefaultBranch_Develop(t *testing.T) {
	g := newGitOps()
	g.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		return []byte("refs/remotes/origin/develop\n"), nil
	}

	branch, err := g.GetDefaultBranch(context.Background())
	if err != nil {
		t.Fatalf("GetDefaultBranch: %v", err)
	}
	if branch != "develop" {
		t.Errorf("branch = %q, want %q", branch, "develop")
	}
}

// --- CheckBranchProtection ---

func TestCheckBranchProtection_NoProtection(t *testing.T) {
	g := newGitOps()
	gc := newGHClient()
	gc.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		return []byte(`{"message":"Not Found"}`),
			fmt.Errorf("exit 1")
	}

	warnings := g.CheckBranchProtection(
		context.Background(), gc, "main",
	)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want empty", warnings)
	}
}

func TestCheckBranchProtection_WithRules(t *testing.T) {
	g := newGitOps()
	gc := newGHClient()
	gc.run = func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		body := `{
			"required_status_checks": {"strict": true},
			"required_pull_request_reviews": {"required_approving_review_count": 1},
			"enforce_admins": {"enabled":true}
		}`
		return []byte(body), nil
	}

	warnings := g.CheckBranchProtection(
		context.Background(), gc, "main",
	)
	if len(warnings) != 3 {
		t.Fatalf("warnings count = %d, want 3: %v",
			len(warnings), warnings)
	}

	wantPhrases := []string{
		"required status checks",
		"PR reviews",
		"enforced for admins",
	}
	for _, phrase := range wantPhrases {
		found := false
		for _, w := range warnings {
			if strings.Contains(w, phrase) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing warning containing %q", phrase)
		}
	}
}
