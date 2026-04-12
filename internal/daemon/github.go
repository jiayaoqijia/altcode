package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// cmdRunner abstracts command execution so tests can mock gh calls.
type cmdRunner func(
	ctx context.Context, dir string, name string, args ...string,
) ([]byte, error)

// defaultCmdRunner shells out via exec.CommandContext.
func defaultCmdRunner(
	ctx context.Context, dir, name string, args ...string,
) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	return cmd.CombinedOutput()
}

// GitHubClient wraps `gh` CLI calls for the daemon.
type GitHubClient struct {
	repoOwner string
	repoName  string
	workDir   string
	logger    *slog.Logger
	run       cmdRunner
}

// NewGitHubClient creates a client targeting the given repo.
func NewGitHubClient(
	owner, name, workDir string, logger *slog.Logger,
) *GitHubClient {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return &GitHubClient{
		repoOwner: owner,
		repoName:  name,
		workDir:   workDir,
		logger:    logger,
		run:       defaultCmdRunner,
	}
}

// repo returns the "owner/name" string used by gh flags.
func (g *GitHubClient) repo() string {
	return g.repoOwner + "/" + g.repoName
}

// CreateDraftPR opens a draft PR and returns the number and URL.
func (g *GitHubClient) CreateDraftPR(
	ctx context.Context, branch, title, body string,
) (prNumber int, prURL string, err error) {
	out, err := g.run(ctx, g.workDir, "gh", "pr", "create",
		"--draft",
		"--head", branch,
		"--title", title,
		"--body", body,
		"--repo", g.repo(),
		"--json", "number,url",
	)
	if err != nil {
		return 0, "", fmt.Errorf(
			"gh pr create: %w: %s", err, string(out),
		)
	}
	var result struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, "", fmt.Errorf(
			"parse pr create output: %w: %s", err, string(out),
		)
	}
	return result.Number, result.URL, nil
}

// UpdatePRBody replaces the PR description.
func (g *GitHubClient) UpdatePRBody(
	ctx context.Context, prNumber int, body string,
) error {
	out, err := g.run(ctx, g.workDir, "gh", "pr", "edit",
		strconv.Itoa(prNumber),
		"--body", body,
		"--repo", g.repo(),
	)
	if err != nil {
		return fmt.Errorf(
			"gh pr edit: %w: %s", err, string(out),
		)
	}
	return nil
}

// ciCheck is the JSON shape returned by gh pr checks.
type ciCheck struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	Conclusion string `json:"conclusion"`
}

// GetCIStatus returns "pass", "fail", or "pending".
func (g *GitHubClient) GetCIStatus(
	ctx context.Context, prNumber int,
) (string, error) {
	out, err := g.run(ctx, g.workDir, "gh", "pr", "checks",
		strconv.Itoa(prNumber),
		"--repo", g.repo(),
		"--json", "name,state,conclusion",
	)
	if err != nil {
		return "", fmt.Errorf(
			"gh pr checks: %w: %s", err, string(out),
		)
	}
	var checks []ciCheck
	if err := json.Unmarshal(out, &checks); err != nil {
		return "", fmt.Errorf(
			"parse pr checks: %w: %s", err, string(out),
		)
	}
	if len(checks) == 0 {
		return "pending", nil
	}
	for _, c := range checks {
		if c.State == "PENDING" || c.State == "QUEUED" ||
			c.State == "IN_PROGRESS" {
			return "pending", nil
		}
	}
	for _, c := range checks {
		if c.Conclusion == "FAILURE" || c.Conclusion == "failure" {
			return "fail", nil
		}
	}
	return "pass", nil
}

// ghRun is the JSON shape for a workflow run.
type ghRun struct {
	DatabaseID int64  `json:"databaseId"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// ghJob is the JSON shape for a job within a run.
type ghJob struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	Steps      []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
	} `json:"steps"`
}

// GetCILogs retrieves failure output from the most recent failed run.
func (g *GitHubClient) GetCILogs(
	ctx context.Context, prNumber int,
) (string, error) {
	// List runs for the PR's head branch.
	out, err := g.run(ctx, g.workDir, "gh", "run", "list",
		"--branch", fmt.Sprintf("pr-%d", prNumber),
		"--repo", g.repo(),
		"--json", "databaseId,status,conclusion",
		"--limit", "1",
	)
	if err != nil {
		return "", fmt.Errorf(
			"gh run list: %w: %s", err, string(out),
		)
	}
	var runs []ghRun
	if err := json.Unmarshal(out, &runs); err != nil {
		return "", fmt.Errorf("parse run list: %w", err)
	}
	if len(runs) == 0 {
		return "", fmt.Errorf("no CI runs found for PR %d", prNumber)
	}

	runID := runs[0].DatabaseID

	// Get failed jobs from this run.
	jobOut, err := g.run(ctx, g.workDir, "gh", "run", "view",
		strconv.FormatInt(runID, 10),
		"--repo", g.repo(),
		"--json", "jobs",
	)
	if err != nil {
		return "", fmt.Errorf(
			"gh run view: %w: %s", err, string(jobOut),
		)
	}

	var jobResult struct {
		Jobs []ghJob `json:"jobs"`
	}
	if err := json.Unmarshal(jobOut, &jobResult); err != nil {
		return "", fmt.Errorf("parse run view: %w", err)
	}

	var failedNames []string
	for _, j := range jobResult.Jobs {
		if j.Conclusion == "failure" {
			failedNames = append(failedNames, j.Name)
		}
	}

	// Fetch the run log (plain text).
	logOut, err := g.run(ctx, g.workDir, "gh", "run", "view",
		strconv.FormatInt(runID, 10),
		"--repo", g.repo(),
		"--log-failed",
	)
	if err != nil {
		// --log-failed may not be available; fall back to summary.
		return fmt.Sprintf(
			"Failed jobs: %s (logs unavailable)",
			strings.Join(failedNames, ", "),
		), nil
	}
	return string(logOut), nil
}

// AddReaction adds an emoji reaction to an issue or PR.
func (g *GitHubClient) AddReaction(
	ctx context.Context, issueNumber int, reaction string,
) error {
	endpoint := fmt.Sprintf(
		"repos/%s/issues/%d/reactions",
		g.repo(), issueNumber,
	)
	out, err := g.run(ctx, g.workDir, "gh", "api", endpoint,
		"-f", "content="+reaction,
	)
	if err != nil {
		return fmt.Errorf(
			"gh api reactions: %w: %s", err, string(out),
		)
	}
	return nil
}

// PostComment posts a comment on an issue or PR.
func (g *GitHubClient) PostComment(
	ctx context.Context, issueNumber int, body string,
) error {
	out, err := g.run(ctx, g.workDir, "gh", "issue", "comment",
		strconv.Itoa(issueNumber),
		"--body", body,
		"--repo", g.repo(),
	)
	if err != nil {
		return fmt.Errorf(
			"gh issue comment: %w: %s", err, string(out),
		)
	}
	return nil
}

// GetDefaultBranch returns the repo's default branch name.
func (g *GitHubClient) GetDefaultBranch(
	ctx context.Context,
) (string, error) {
	endpoint := fmt.Sprintf("repos/%s", g.repo())
	out, err := g.run(ctx, g.workDir, "gh", "api", endpoint,
		"--jq", ".default_branch",
	)
	if err != nil {
		return "", fmt.Errorf(
			"gh api default branch: %w: %s", err, string(out),
		)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "", fmt.Errorf("empty default branch from gh api")
	}
	return branch, nil
}

// BuildPRBody generates a structured PR description from the task,
// plan, and diff summary.
func BuildPRBody(
	task *Task, plan *Plan, diffSummary string,
) string {
	var b strings.Builder

	b.WriteString("## Summary\n\n")
	b.WriteString(task.TaskDescription)
	b.WriteString("\n\n")

	if plan != nil && len(plan.Steps) > 0 {
		b.WriteString("## Changes\n\n")
		for i, step := range plan.Steps {
			b.WriteString(fmt.Sprintf(
				"%d. %s\n", i+1, step.Description,
			))
		}
		b.WriteString("\n")
	}

	if diffSummary != "" {
		b.WriteString("## Diff Summary\n\n")
		b.WriteString("```\n")
		b.WriteString(diffSummary)
		b.WriteString("\n```\n\n")
	}

	b.WriteString("## Task Checklist\n\n")
	b.WriteString("- [ ] CI passes\n")
	b.WriteString("- [ ] Code review approved\n")
	b.WriteString("- [ ] Tests cover changes\n")

	if task.IssueNumber > 0 {
		b.WriteString(fmt.Sprintf(
			"\n\nFixes #%d\n", task.IssueNumber,
		))
	}

	return b.String()
}

// waitForCI polls GetCIStatus until it returns "pass" or "fail",
// or the timeout elapses (returns "pending").
func (g *GitHubClient) waitForCI(
	ctx context.Context, prNumber int, timeout time.Duration,
) string {
	deadline := time.After(timeout)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Check once immediately before waiting.
	if status, err := g.GetCIStatus(ctx, prNumber); err == nil {
		if status == "pass" || status == "fail" {
			return status
		}
	}

	for {
		select {
		case <-ctx.Done():
			return "pending"
		case <-deadline:
			return "pending"
		case <-ticker.C:
			status, err := g.GetCIStatus(ctx, prNumber)
			if err != nil {
				g.logger.Warn("CI poll error",
					"pr", prNumber, "err", err)
				continue
			}
			if status == "pass" || status == "fail" {
				return status
			}
		}
	}
}

// RunCIAutofix polls CI, reads failure logs, and spawns a fix agent.
// Returns nil when CI passes or an error after maxAttempts.
func (g *GitHubClient) RunCIAutofix(
	ctx context.Context,
	prNumber int,
	maxAttempts int,
	spawnFix SpawnFunc,
) error {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		status := g.waitForCI(ctx, prNumber, 10*time.Minute)
		if status == "pass" {
			return nil
		}
		if status == "pending" {
			return fmt.Errorf("CI timed out after %d min", 10)
		}

		logs, err := g.GetCILogs(ctx, prNumber)
		if err != nil {
			g.logger.Warn("failed to fetch CI logs",
				"pr", prNumber, "attempt", attempt, "err", err)
			logs = "(CI logs unavailable)"
		}

		fixPrompt := BuildAutofixPrompt(logs, "")
		_, spawnErr := spawnFix(ctx, AgentConfig{
			Role: "autofix",
			Args: []string{fixPrompt},
		})
		if spawnErr != nil {
			g.logger.Warn("autofix agent failed",
				"pr", prNumber, "attempt", attempt,
				"err", spawnErr)
			continue
		}
	}
	return fmt.Errorf(
		"CI autofix: %d attempts exhausted", maxAttempts,
	)
}
