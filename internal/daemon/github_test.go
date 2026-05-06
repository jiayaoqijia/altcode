package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// mockRunner returns a cmdRunner that maps (binary, args...) to
// canned responses. Unmatched calls return an error.
func mockRunner(
	responses map[string]struct {
		out []byte
		err error
	},
) cmdRunner {
	return func(
		_ context.Context, _ string, name string, args ...string,
	) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		for prefix, resp := range responses {
			if strings.HasPrefix(key, prefix) {
				return resp.out, resp.err
			}
		}
		return nil, fmt.Errorf("unexpected command: %s", key)
	}
}

type mockResp struct {
	out []byte
	err error
}

func newGHClient() *GitHubClient {
	return NewGitHubClient("test-owner", "test-repo", "/tmp", nil)
}

func TestGitHubClient_CreateDraftPR(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr create": {
			out: []byte(`{"number":42,"url":"https://github.com/test-owner/test-repo/pull/42"}`),
		},
	})

	num, url, err := gc.CreateDraftPR(
		context.Background(), "feat/auth", "Add auth", "body",
	)
	if err != nil {
		t.Fatalf("CreateDraftPR: %v", err)
	}
	if num != 42 {
		t.Errorf("prNumber = %d, want 42", num)
	}
	if !strings.Contains(url, "/pull/42") {
		t.Errorf("prURL = %q, want /pull/42", url)
	}
}

func TestGitHubClient_CreateDraftPR_Error(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr create": {
			out: []byte("permission denied"),
			err: fmt.Errorf("exit 1"),
		},
	})

	_, _, err := gc.CreateDraftPR(
		context.Background(), "feat/x", "Title", "Body",
	)
	if err == nil {
		t.Fatal("expected error from failed gh pr create")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %q, want mention of stderr", err)
	}
}

func TestGitHubClient_GetCIStatus_Pass(t *testing.T) {
	gc := newGHClient()
	checks := []ciCheck{
		{Name: "build", State: "SUCCESS"},
		{Name: "lint", State: "SUCCESS"},
	}
	data, _ := json.Marshal(checks)
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr checks": {out: data},
	})

	status, err := gc.GetCIStatus(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetCIStatus: %v", err)
	}
	if status != "pass" {
		t.Errorf("status = %q, want %q", status, "pass")
	}
}

func TestGitHubClient_GetCIStatus_Fail(t *testing.T) {
	gc := newGHClient()
	checks := []ciCheck{
		{Name: "build", State: "SUCCESS"},
		{Name: "test", State: "FAILURE"},
	}
	data, _ := json.Marshal(checks)
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr checks": {out: data},
	})

	status, err := gc.GetCIStatus(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetCIStatus: %v", err)
	}
	if status != "fail" {
		t.Errorf("status = %q, want %q", status, "fail")
	}
}

func TestGitHubClient_GetCIStatus_Pending(t *testing.T) {
	gc := newGHClient()
	checks := []ciCheck{
		{Name: "build", State: "IN_PROGRESS"},
	}
	data, _ := json.Marshal(checks)
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr checks": {out: data},
	})

	status, err := gc.GetCIStatus(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetCIStatus: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want %q", status, "pending")
	}
}

func TestGitHubClient_GetCIStatus_Empty(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr checks": {out: []byte("[]")},
	})

	status, err := gc.GetCIStatus(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetCIStatus: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want %q", status, "pending")
	}
}

func TestGitHubClient_GetDefaultBranch(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh api repos/test-owner/test-repo": {out: []byte("main\n")},
	})

	branch, err := gc.GetDefaultBranch(context.Background())
	if err != nil {
		t.Fatalf("GetDefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want %q", branch, "main")
	}
}

func TestGitHubClient_GetDefaultBranch_Empty(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh api repos/test-owner/test-repo": {out: []byte("  \n")},
	})

	_, err := gc.GetDefaultBranch(context.Background())
	if err == nil {
		t.Fatal("expected error for empty default branch")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want mention of empty", err)
	}
}

func TestGitHubClient_AddReaction(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh api repos/test-owner/test-repo/issues/7/reactions": {
			out: []byte(`{"id":1}`),
		},
	})

	err := gc.AddReaction(context.Background(), 7, "+1")
	if err != nil {
		t.Fatalf("AddReaction: %v", err)
	}
}

func TestGitHubClient_PostComment(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh issue comment": {out: []byte("ok")},
	})

	err := gc.PostComment(
		context.Background(), 7, "working on it",
	)
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
}

func TestGitHubClient_UpdatePRBody(t *testing.T) {
	gc := newGHClient()
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr edit": {out: []byte("")},
	})

	err := gc.UpdatePRBody(
		context.Background(), 42, "updated body",
	)
	if err != nil {
		t.Fatalf("UpdatePRBody: %v", err)
	}
}

func TestBuildPRBody_Full(t *testing.T) {
	task := &Task{
		TaskDescription: "Add user authentication with JWT",
		IssueNumber:     7,
	}
	plan := &Plan{Steps: []PlanStep{
		{Description: "Add login endpoint"},
		{Description: "Add JWT middleware"},
		{Description: "Write tests"},
	}}
	diffSummary := "3 files changed, 120 insertions(+), 5 deletions(-)"

	body := BuildPRBody(task, plan, diffSummary)

	for _, want := range []string{
		"## Summary",
		"Add user authentication with JWT",
		"## Changes",
		"1. Add login endpoint",
		"2. Add JWT middleware",
		"3. Write tests",
		"## Diff Summary",
		"3 files changed",
		"## Task Checklist",
		"- [ ] CI passes",
		"- [ ] Code review approved",
		"- [ ] Tests cover changes",
		"Fixes #7",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestBuildPRBody_NoPlan(t *testing.T) {
	task := &Task{
		TaskDescription: "Quick fix",
		IssueNumber:     0,
	}
	body := BuildPRBody(task, nil, "")

	if !strings.Contains(body, "## Summary") {
		t.Error("missing Summary section")
	}
	if strings.Contains(body, "## Changes") {
		t.Error("Changes section should be absent with nil plan")
	}
	if strings.Contains(body, "## Diff Summary") {
		t.Error("Diff Summary should be absent with empty diff")
	}
	if strings.Contains(body, "Fixes #") {
		t.Error("Fixes link should be absent when IssueNumber=0")
	}
	if !strings.Contains(body, "## Task Checklist") {
		t.Error("missing Task Checklist section")
	}
}

func TestBuildPRBody_EmptySteps(t *testing.T) {
	task := &Task{TaskDescription: "Refactor"}
	plan := &Plan{Steps: []PlanStep{}}
	body := BuildPRBody(task, plan, "")

	if strings.Contains(body, "## Changes") {
		t.Error("Changes section should be absent with empty steps")
	}
}

func TestRunCIAutofix_PassesFirst(t *testing.T) {
	gc := newGHClient()

	// CI passes on first check — no fix agent needed.
	checks := []ciCheck{
		{Name: "build", State: "SUCCESS"},
	}
	data, _ := json.Marshal(checks)
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr checks": {out: data},
	})

	var fixCalls int32
	spawnFix := func(
		_ context.Context, _ AgentConfig,
	) (string, error) {
		atomic.AddInt32(&fixCalls, 1)
		return "fixed", nil
	}

	err := gc.RunCIAutofix(context.Background(), 42, "fix/auth", 3, spawnFix)
	if err != nil {
		t.Fatalf("RunCIAutofix: %v", err)
	}
	if atomic.LoadInt32(&fixCalls) != 0 {
		t.Errorf("fix calls = %d, want 0", fixCalls)
	}
}

func TestRunCIAutofix_FixesOnRetry(t *testing.T) {
	gc := newGHClient()

	// First call: fail. Second call: pass.
	var ciCalls int32
	failChecks, _ := json.Marshal([]ciCheck{
		{Name: "test", State: "FAILURE"},
	})
	passChecks, _ := json.Marshal([]ciCheck{
		{Name: "test", State: "SUCCESS"},
	})
	runList, _ := json.Marshal([]ghRun{
		{DatabaseID: 100, Status: "completed", Conclusion: "failure"},
	})
	runView, _ := json.Marshal(struct {
		Jobs []ghJob `json:"jobs"`
	}{
		Jobs: []ghJob{{Name: "test", Conclusion: "failure"}},
	})

	gc.run = func(
		ctx context.Context, dir, name string, args ...string,
	) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.HasPrefix(key, "gh pr checks") {
			n := atomic.AddInt32(&ciCalls, 1)
			if n <= 1 {
				return failChecks, nil
			}
			return passChecks, nil
		}
		if strings.HasPrefix(key, "gh run list") {
			return runList, nil
		}
		if strings.HasPrefix(key, "gh run view") {
			if strings.Contains(key, "--log-failed") {
				return []byte("FAIL TestAuth"), nil
			}
			return runView, nil
		}
		return nil, fmt.Errorf("unexpected: %s", key)
	}

	var fixCalls int32
	spawnFix := func(
		_ context.Context, cfg AgentConfig,
	) (string, error) {
		atomic.AddInt32(&fixCalls, 1)
		if cfg.Role != "autofix" {
			t.Errorf("role = %q, want autofix", cfg.Role)
		}
		return "fixed", nil
	}

	err := gc.RunCIAutofix(context.Background(), 42, "altfix/auth", 3, spawnFix)
	if err != nil {
		t.Fatalf("RunCIAutofix: %v", err)
	}
	if n := atomic.LoadInt32(&fixCalls); n != 1 {
		t.Errorf("fix calls = %d, want 1", n)
	}
}

func TestRunCIAutofix_ExhaustsAttempts(t *testing.T) {
	gc := newGHClient()

	failChecks, _ := json.Marshal([]ciCheck{
		{Name: "test", State: "FAILURE"},
	})
	runList, _ := json.Marshal([]ghRun{
		{DatabaseID: 200, Status: "completed", Conclusion: "failure"},
	})
	runView, _ := json.Marshal(struct {
		Jobs []ghJob `json:"jobs"`
	}{
		Jobs: []ghJob{{Name: "test", Conclusion: "failure"}},
	})

	gc.run = func(
		ctx context.Context, dir, name string, args ...string,
	) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.HasPrefix(key, "gh pr checks") {
			return failChecks, nil
		}
		if strings.HasPrefix(key, "gh run list") {
			return runList, nil
		}
		if strings.HasPrefix(key, "gh run view") {
			if strings.Contains(key, "--log-failed") {
				return []byte("FAIL: compilation error"), nil
			}
			return runView, nil
		}
		return nil, fmt.Errorf("unexpected: %s", key)
	}

	var fixCalls int32
	spawnFix := func(
		_ context.Context, _ AgentConfig,
	) (string, error) {
		atomic.AddInt32(&fixCalls, 1)
		return "attempted fix", nil
	}

	err := gc.RunCIAutofix(context.Background(), 42, "fix/compile", 2, spawnFix)
	if err == nil {
		t.Fatal("expected error when attempts exhausted")
	}
	if !strings.Contains(err.Error(), "2 attempts exhausted") {
		t.Errorf("error = %q, want mention of exhausted", err)
	}
	if n := atomic.LoadInt32(&fixCalls); n != 2 {
		t.Errorf("fix calls = %d, want 2", n)
	}
}

func TestRunCIAutofix_ContextCancelled(t *testing.T) {
	gc := newGHClient()

	// CI always pending, context cancelled immediately.
	pendingChecks, _ := json.Marshal([]ciCheck{
		{Name: "build", State: "PENDING"},
	})
	gc.run = mockRunner(map[string]struct {
		out []byte
		err error
	}{
		"gh pr checks": {out: pendingChecks},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := gc.RunCIAutofix(ctx, 42, "fix/pending", 3, func(
		_ context.Context, _ AgentConfig,
	) (string, error) {
		return "", nil
	})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want timeout mention", err)
	}
}

func TestGitHubClient_GetCILogs_FallbackOnLogFailed(t *testing.T) {
	gc := newGHClient()

	runList, _ := json.Marshal([]ghRun{
		{DatabaseID: 300, Status: "completed", Conclusion: "failure"},
	})
	runView, _ := json.Marshal(struct {
		Jobs []ghJob `json:"jobs"`
	}{
		Jobs: []ghJob{{Name: "lint", Conclusion: "failure"}},
	})

	gc.run = func(
		ctx context.Context, dir, name string, args ...string,
	) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if strings.HasPrefix(key, "gh run list") {
			return runList, nil
		}
		if strings.HasPrefix(key, "gh run view") {
			if strings.Contains(key, "--log-failed") {
				return nil, fmt.Errorf("exit 1")
			}
			return runView, nil
		}
		return nil, fmt.Errorf("unexpected: %s", key)
	}

	logs, err := gc.GetCILogs(context.Background(), 10, "fix/lint")
	if err != nil {
		t.Fatalf("GetCILogs: %v", err)
	}
	if !strings.Contains(logs, "lint") {
		t.Errorf("fallback logs = %q, want failed job name", logs)
	}
	if !strings.Contains(logs, "logs unavailable") {
		t.Errorf("fallback logs = %q, want unavailable note", logs)
	}
}
