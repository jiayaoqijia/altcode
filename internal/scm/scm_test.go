package scm

import (
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

func TestParsePRJSON(t *testing.T) {
	raw := `{
		"number": 42,
		"title": "Add counter",
		"url": "https://github.com/foo/bar/pull/42",
		"headRefName": "altcode/impl/counter",
		"baseRefName": "main",
		"state": "OPEN",
		"isDraft": true,
		"headRefOid": "abc123",
		"createdAt": "2025-06-01T10:00:00Z",
		"updatedAt": "2025-06-01T12:00:00Z",
		"mergeable": "MERGEABLE",
		"reviewDecision": "APPROVED",
		"statusCheckRollup": [
			{"name":"ci","status":"COMPLETED","conclusion":"SUCCESS"}
		]
	}`

	pr, err := parsePRJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertPRFields(t, pr)
}

func assertPRFields(t *testing.T, pr *workspace.PR) {
	t.Helper()
	assertEqual(t, "Number", pr.Number, 42)
	assertEqual(t, "Title", pr.Title, "Add counter")
	assertEqual(t, "HeadBranch", pr.HeadBranch, "altcode/impl/counter")
	assertEqual(t, "BaseBranch", pr.BaseBranch, "main")
	assertEqual(t, "State", pr.State, "open")
	assertEqual(t, "Draft", pr.Draft, true)
	assertEqual(t, "HeadSHA", pr.HeadSHA, "abc123")
	assertEqual(t, "ReviewStatus", pr.ReviewStatus, workspace.ReviewApproved)
	assertEqual(t, "CIStatus", pr.CIStatus, workspace.CIPass)
	assertEqual(t, "Mergeable", pr.MergeableState, "MERGEABLE")
}

func TestCIStatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect workspace.CICheckStatus
	}{
		{
			name:   "all pass",
			input:  allPassJSON,
			expect: workspace.CIPass,
		},
		{
			name:   "one failure",
			input:  oneFailJSON,
			expect: workspace.CIFail,
		},
		{
			name:   "still running",
			input:  runningJSON,
			expect: workspace.CIRunning,
		},
		{
			name:   "empty checks",
			input:  `{"check_runs":[]}`,
			expect: workspace.CIUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCheckRunsStatus(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertEqual(t, "CIStatus", got, tc.expect)
		})
	}
}

func TestListPRsFilter(t *testing.T) {
	raw := `[
		{"number":1,"title":"A","headRefName":"altcode/impl/a",
		 "baseRefName":"main","state":"OPEN","headRefOid":"aaa",
		 "createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"},
		{"number":2,"title":"B","headRefName":"feature/b",
		 "baseRefName":"main","state":"OPEN","headRefOid":"bbb",
		 "createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"},
		{"number":3,"title":"C","headRefName":"altcode/arch/c",
		 "baseRefName":"main","state":"OPEN","headRefOid":"ccc",
		 "createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}
	]`

	prs, err := filterPRsByHead(raw, "altcode/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, "count", len(prs), 2)
	assertEqual(t, "first", prs[0].Number, 1)
	assertEqual(t, "second", prs[1].Number, 3)
}

func TestNoopSCM(t *testing.T) {
	var s workspace.SCM = &NoopSCM{}

	assertEqual(t, "name", s.Name(), "none")
	assertNoopReturns(t, s)
}

func assertNoopReturns(t *testing.T, s workspace.SCM) {
	t.Helper()
	ctx := t.Context()

	pr, err := s.CreatePR(ctx, workspace.CreatePRRequest{})
	assertPRNil(t, "CreatePR", pr)
	assertErrNil(t, "CreatePR", err)

	pr, err = s.GetPR(ctx, 1)
	assertPRNil(t, "GetPR", pr)
	assertErrNil(t, "GetPR", err)

	prs, err := s.ListPRs(ctx, "x")
	assertSliceNil(t, "ListPRs", prs)
	assertErrNil(t, "ListPRs", err)

	reviews, err := s.GetPRReviews(ctx, 1)
	assertReviewsNil(t, "GetPRReviews", reviews)
	assertErrNil(t, "GetPRReviews", err)

	assertErrNil(t, "RequestReview", s.RequestReview(ctx, 1, nil))
	assertErrNil(t, "MergePR", s.MergePR(ctx, 1, workspace.MergeSquash))

	ci, err := s.CIStatus(ctx, "sha")
	assertEqual(t, "CIStatus", ci, workspace.CIUnknown)
	assertErrNil(t, "CIStatus", err)
}

func TestReviewsParsing(t *testing.T) {
	raw := `{
		"reviews": [
			{
				"id": 1,
				"author": {"login": "alice"},
				"state": "CHANGES_REQUESTED",
				"body": "fix the thing",
				"submittedAt": "2025-06-01T10:00:00Z"
			}
		],
		"comments": [
			{
				"author": {"login": "alice"},
				"body": "this line is wrong",
				"path": "main.go",
				"line": 42
			}
		]
	}`

	reviews, err := parseReviewsJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, "count", len(reviews), 1)
	assertEqual(t, "author", reviews[0].Author, "alice")
	assertEqual(t, "state", reviews[0].State, "CHANGES_REQUESTED")
	assertEqual(t, "comments", len(reviews[0].Comments), 1)
	assertEqual(t, "comment.path", reviews[0].Comments[0].Path, "main.go")
	assertEqual(t, "comment.line", reviews[0].Comments[0].Line, 42)
}

func TestMapReviewDecision(t *testing.T) {
	tests := []struct {
		in  string
		out workspace.ReviewStatus
	}{
		{"APPROVED", workspace.ReviewApproved},
		{"CHANGES_REQUESTED", workspace.ReviewChangesRequested},
		{"REVIEW_REQUIRED", workspace.ReviewNone},
		{"", workspace.ReviewNone},
	}
	for _, tc := range tests {
		got := mapReviewDecision(tc.in)
		assertEqual(t, tc.in, got, tc.out)
	}
}

func TestMapPRState(t *testing.T) {
	assertEqual(t, "open", mapPRState("OPEN"), "open")
	assertEqual(t, "merged", mapPRState("MERGED"), "merged")
	assertEqual(t, "closed", mapPRState("CLOSED"), "closed")
}

func TestMergeMethodFlag(t *testing.T) {
	assertEqual(t, "squash", mergeMethodFlag(workspace.MergeSquash), "--squash")
	assertEqual(t, "rebase", mergeMethodFlag(workspace.MergeRebase), "--rebase")
	assertEqual(t, "merge", mergeMethodFlag(workspace.MergeMerge), "--merge")
}

func TestCILogs(t *testing.T) {
	raw := `{
		"check_runs": [
			{
				"name": "build",
				"status": "completed",
				"conclusion": "failure",
				"output": {
					"title": "Build failed",
					"summary": "exit code 1",
					"text": "error: undefined var"
				}
			},
			{
				"name": "lint",
				"status": "completed",
				"conclusion": "success",
				"output": {"title":"OK","summary":"clean","text":""}
			}
		]
	}`
	logs, err := extractFailingLogs(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, logs, "=== build ===")
	assertContains(t, logs, "exit code 1")
	assertContains(t, logs, "error: undefined var")
	assertNotContains(t, logs, "=== lint ===")
}

func TestBuildCreatePRArgs(t *testing.T) {
	req := workspace.CreatePRRequest{
		Title:  "feat: add X",
		Body:   "desc",
		Head:   "altcode/impl/x",
		Base:   "main",
		Draft:  true,
		Labels: []string{"auto", "ci"},
	}
	args := buildCreatePRArgs(req)
	assertContainsStr(t, args, "--draft")
	assertContainsStr(t, args, "--label")
}

func TestRollupToStatusEmpty(t *testing.T) {
	got := rollupToStatus(nil)
	assertEqual(t, "empty", got, workspace.CIUnknown)
}

func TestTruncateLog(t *testing.T) {
	short := "hello"
	assertEqual(t, "short", truncateLog(short), short)

	long := make([]byte, maxCILogBytes+100)
	for i := range long {
		long[i] = 'x'
	}
	result := truncateLog(string(long))
	assertContains(t, result, "[log truncated]")
}

// --- test fixtures --------------------------------------------------

var _ time.Time // ensure time import used

const allPassJSON = `{"check_runs":[
	{"name":"ci","status":"completed","conclusion":"success"},
	{"name":"lint","status":"completed","conclusion":"success"}
]}`

const oneFailJSON = `{"check_runs":[
	{"name":"ci","status":"completed","conclusion":"success"},
	{"name":"test","status":"completed","conclusion":"failure"}
]}`

const runningJSON = `{"check_runs":[
	{"name":"ci","status":"in_progress","conclusion":""},
	{"name":"test","status":"completed","conclusion":"success"}
]}`

// --- test helpers ---------------------------------------------------

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertErrNil(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: expected nil error, got %v", name, err)
	}
}

func assertPRNil(t *testing.T, name string, pr *workspace.PR) {
	t.Helper()
	if pr != nil {
		t.Errorf("%s: expected nil PR, got %v", name, pr)
	}
}

func assertSliceNil(t *testing.T, name string, prs []*workspace.PR) {
	t.Helper()
	if prs != nil {
		t.Errorf("%s: expected nil slice, got %v", name, prs)
	}
}

func assertReviewsNil(t *testing.T, name string, rs []*workspace.Review) {
	t.Helper()
	if rs != nil {
		t.Errorf("%s: expected nil slice, got %v", name, rs)
	}
}

func assertContains(t *testing.T, s, sub string) {
	t.Helper()
	if !contains(s, sub) {
		t.Errorf("expected %q to contain %q", s, sub)
	}
}

func assertNotContains(t *testing.T, s, sub string) {
	t.Helper()
	if contains(s, sub) {
		t.Errorf("expected %q to NOT contain %q", s, sub)
	}
}

func assertContainsStr(t *testing.T, ss []string, want string) {
	t.Helper()
	for _, s := range ss {
		if s == want {
			return
		}
	}
	t.Errorf("expected %v to contain %q", ss, want)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
