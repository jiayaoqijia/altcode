package daemon

import (
	"strings"
	"testing"
)

func TestBuildPlanPrompt(t *testing.T) {
	task := &Task{
		RepoURL:         "https://github.com/test/repo",
		TaskDescription: "add user authentication",
	}
	out := BuildPlanPrompt(task, "Go project, 20 packages")

	if out == "" {
		t.Fatal("output is empty")
	}
	for _, want := range []string{
		task.RepoURL,
		task.TaskDescription,
		"Go project, 20 packages",
		"JSON plan",
		"success_criteria",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestBuildImplementPrompt(t *testing.T) {
	step := PlanStep{
		Description: "add login endpoint",
		Prompt:      "implement POST /login with JWT",
	}
	out := BuildImplementPrompt(step, "internal/auth/auth.go")

	if out == "" {
		t.Fatal("output is empty")
	}
	for _, want := range []string{
		step.Description,
		step.Prompt,
		"internal/auth/auth.go",
		"Run tests",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestBuildReviewPrompt(t *testing.T) {
	out := BuildReviewPrompt(
		"diff --git a/main.go b/main.go\n+added line",
		"add auth feature",
		"tests pass, no regressions",
	)

	if out == "" {
		t.Fatal("output is empty")
	}
	for _, want := range []string{
		"diff --git",
		"add auth feature",
		"tests pass, no regressions",
		`"verdict"`,
		`"pass"`,
		`"fail"`,
		"JSON",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestBuildTestPrompt(t *testing.T) {
	out := BuildTestPrompt("auth.go\nauth_test.go", "go test")

	if out == "" {
		t.Fatal("output is empty")
	}
	for _, want := range []string{
		"auth.go",
		"go test",
		"happy path",
		"error case",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestBuildAutofixPrompt(t *testing.T) {
	out := BuildAutofixPrompt(
		"FAIL TestAuth: expected 200 got 401",
		"internal/auth/handler.go",
	)

	if out == "" {
		t.Fatal("output is empty")
	}
	for _, want := range []string{
		"FAIL TestAuth",
		"internal/auth/handler.go",
		"CI logs",
		"root cause",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestBuildSteerPrompt(t *testing.T) {
	out := BuildSteerPrompt(
		"actually use OAuth instead of JWT",
		`{"steps":[{"description":"add JWT"}]}`,
		"step 0 completed",
	)

	if out == "" {
		t.Fatal("output is empty")
	}
	for _, want := range []string{
		"actually use OAuth instead of JWT",
		"add JWT",
		"step 0 completed",
		"JSON plan",
		"user feedback",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}
