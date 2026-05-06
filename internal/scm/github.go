package scm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

// shaPattern matches a valid git SHA: 7-40 lowercase hex characters.
// Used to validate commit SHAs before embedding them in gh CLI API
// endpoint paths, preventing injection of gh flags or path traversal.
var shaPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

func validateSHA(sha string) error {
	if !shaPattern.MatchString(sha) {
		return fmt.Errorf("invalid commit SHA %q (must be 7-40 hex chars)", sha)
	}
	return nil
}

// GitHubCLISCM implements workspace.SCM using the gh CLI.
type GitHubCLISCM struct {
	owner string
	repo  string
}

// NewGitHubSCM returns a GitHub SCM, preferring the direct API when a
// token is available and falling back to the gh CLI.
func NewGitHubSCM() (workspace.SCM, error) {
	owner, repo, err := DetectOwnerRepo()
	if err != nil {
		// Fallback: try gh CLI detection.
		owner, repo, err = detectRepoInfo()
		if err != nil {
			return nil, fmt.Errorf("github scm: %w", err)
		}
	}
	token := DiscoverGitHubToken()
	if token != "" {
		api, aerr := NewGitHubAPISCMWithToken(owner, repo, token)
		if aerr == nil {
			return api, nil
		}
	}
	return &GitHubCLISCM{owner: owner, repo: repo}, nil
}

// NewGitHubCLISCM creates a GitHubCLISCM, auto-detecting owner/repo
// from the gh CLI. Kept as a fallback when no API token is available.
func NewGitHubCLISCM() (*GitHubCLISCM, error) {
	owner, repo, err := detectRepoInfo()
	if err != nil {
		return nil, fmt.Errorf("github cli scm: %w", err)
	}
	return &GitHubCLISCM{owner: owner, repo: repo}, nil
}

// newGitHubSCMExplicit creates a GitHubCLISCM with explicit owner/repo.
func newGitHubSCMExplicit(owner, repo string) *GitHubCLISCM {
	return &GitHubCLISCM{owner: owner, repo: repo}
}

func (g *GitHubCLISCM) Name() string { return "github" }

// CreatePR creates a pull request via gh pr create.
func (g *GitHubCLISCM) CreatePR(
	ctx context.Context, req workspace.CreatePRRequest,
) (*workspace.PR, error) {
	args := buildCreatePRArgs(req)
	out, err := runGH(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("create pr: %w", err)
	}
	return g.parsePRFromURL(ctx, strings.TrimSpace(out))
}

func buildCreatePRArgs(req workspace.CreatePRRequest) []string {
	args := []string{
		"pr", "create",
		"--title", req.Title,
		"--body", req.Body,
		"--head", req.Head,
		"--base", req.Base,
	}
	if req.Draft {
		args = append(args, "--draft")
	}
	for _, l := range req.Labels {
		args = append(args, "--label", l)
	}
	return args
}

// parsePRFromURL fetches PR data from a URL returned by gh pr create.
func (g *GitHubCLISCM) parsePRFromURL(
	ctx context.Context, url string,
) (*workspace.PR, error) {
	out, err := runGH(ctx, "pr", "view", url, "--json", prViewFields)
	if err != nil {
		return nil, fmt.Errorf("parse pr url: %w", err)
	}
	return parsePRJSON(out)
}

// GetPR fetches a PR by number.
func (g *GitHubCLISCM) GetPR(
	ctx context.Context, id int,
) (*workspace.PR, error) {
	out, err := runGH(ctx, "pr", "view", fmt.Sprint(id), "--json", prViewFields)
	if err != nil {
		return nil, fmt.Errorf("get pr %d: %w", id, err)
	}
	return parsePRJSON(out)
}

// ListPRs returns open PRs whose head branch starts with headPrefix.
func (g *GitHubCLISCM) ListPRs(
	ctx context.Context, headPrefix string,
) ([]*workspace.PR, error) {
	out, err := runGH(ctx,
		"pr", "list",
		"--state", "open",
		"--json", prViewFields,
		"--limit", "100",
	)
	if err != nil {
		return nil, fmt.Errorf("list prs: %w", err)
	}
	return filterPRsByHead(out, headPrefix)
}

// GetPRReviews returns reviews on a PR.
func (g *GitHubCLISCM) GetPRReviews(
	ctx context.Context, prID int,
) ([]*workspace.Review, error) {
	out, err := runGH(ctx,
		"pr", "view", fmt.Sprint(prID),
		"--json", "reviews,comments",
	)
	if err != nil {
		return nil, fmt.Errorf("get reviews %d: %w", prID, err)
	}
	return parseReviewsJSON(out)
}

// RequestReview requests review from the given reviewers.
func (g *GitHubCLISCM) RequestReview(
	ctx context.Context, prID int, reviewers []string,
) error {
	if len(reviewers) == 0 {
		return nil
	}
	args := []string{
		"pr", "edit", fmt.Sprint(prID),
		"--add-reviewer", strings.Join(reviewers, ","),
	}
	_, err := runGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("request review %d: %w", prID, err)
	}
	return nil
}

// MergePR merges a PR using the given method.
func (g *GitHubCLISCM) MergePR(
	ctx context.Context, prID int, method workspace.MergeMethod,
) error {
	flag := mergeMethodFlag(method)
	_, err := runGH(ctx, "pr", "merge", fmt.Sprint(prID), flag)
	if err != nil {
		return fmt.Errorf("merge pr %d: %w", prID, err)
	}
	return nil
}

// CIStatus returns the combined CI status for a commit SHA.
func (g *GitHubCLISCM) CIStatus(
	ctx context.Context, sha string,
) (workspace.CICheckStatus, error) {
	if err := validateSHA(sha); err != nil {
		return workspace.CIUnknown, err
	}
	endpoint := fmt.Sprintf(
		"repos/%s/%s/commits/%s/check-runs",
		g.owner, g.repo, sha,
	)
	out, err := runGH(ctx, "api", endpoint)
	if err != nil {
		return workspace.CIUnknown, fmt.Errorf("ci status: %w", err)
	}
	return parseCheckRunsStatus(out)
}

// CILogs fetches combined log output from failing checks (max 8KB).
func (g *GitHubCLISCM) CILogs(
	ctx context.Context, sha string,
) (string, error) {
	// Mirror the validation that CIStatus already does — without it
	// an attacker-supplied sha could redirect the gh api request to
	// an arbitrary endpoint.
	if err := validateSHA(sha); err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf(
		"repos/%s/%s/commits/%s/check-runs",
		g.owner, g.repo, sha,
	)
	out, err := runGH(ctx, "api", endpoint)
	if err != nil {
		return "", fmt.Errorf("ci logs: %w", err)
	}
	return extractFailingLogs(out)
}

// --- helpers --------------------------------------------------------

const prViewFields = "number,title,url,headRefName,baseRefName," +
	"state,isDraft,headRefOid,createdAt,updatedAt," +
	"mergeable,reviewDecision,statusCheckRollup"

func mergeMethodFlag(m workspace.MergeMethod) string {
	switch m {
	case workspace.MergeSquash:
		return "--squash"
	case workspace.MergeRebase:
		return "--rebase"
	default:
		return "--merge"
	}
}

// runGH executes gh with args and returns stdout.
func runGH(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// parseGHJSON unmarshals JSON output into target.
func parseGHJSON(output string, target any) error {
	return json.Unmarshal([]byte(output), target)
}

// detectRepoInfo reads owner and repo from gh repo view.
func detectRepoInfo() (string, string, error) {
	out, err := runGH(context.Background(),
		"repo", "view", "--json", "owner,name",
	)
	if err != nil {
		return "", "", err
	}
	var info struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}
	if err := parseGHJSON(out, &info); err != nil {
		return "", "", fmt.Errorf("parse repo info: %w", err)
	}
	return info.Owner.Login, info.Name, nil
}

// --- JSON parsing ---------------------------------------------------

// ghPRRaw matches gh pr view --json output.
type ghPRRaw struct {
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	HeadRefName    string    `json:"headRefName"`
	BaseRefName    string    `json:"baseRefName"`
	State          string    `json:"state"`
	IsDraft        bool      `json:"isDraft"`
	HeadRefOid     string    `json:"headRefOid"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Mergeable      string    `json:"mergeable"`
	ReviewDecision string    `json:"reviewDecision"`
	StatusChecks   []ghCheck `json:"statusCheckRollup"`
}

type ghCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// parsePRJSON parses a single PR from gh pr view --json output.
func parsePRJSON(output string) (*workspace.PR, error) {
	var raw ghPRRaw
	if err := parseGHJSON(output, &raw); err != nil {
		return nil, fmt.Errorf("parse pr json: %w", err)
	}
	return convertRawPR(&raw), nil
}

func convertRawPR(raw *ghPRRaw) *workspace.PR {
	return &workspace.PR{
		ID:             raw.Number,
		Number:         raw.Number,
		Title:          raw.Title,
		URL:            raw.URL,
		HeadSHA:        raw.HeadRefOid,
		HeadBranch:     raw.HeadRefName,
		BaseBranch:     raw.BaseRefName,
		State:          mapPRState(raw.State),
		Draft:          raw.IsDraft,
		CIStatus:       rollupToStatus(raw.StatusChecks),
		ReviewStatus:   mapReviewDecision(raw.ReviewDecision),
		MergeableState: raw.Mergeable,
		CreatedAt:      raw.CreatedAt,
		UpdatedAt:      raw.UpdatedAt,
	}
}

func mapPRState(state string) string {
	switch strings.ToUpper(state) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	default:
		return "open"
	}
}

func mapReviewDecision(decision string) workspace.ReviewStatus {
	switch strings.ToUpper(decision) {
	case "APPROVED":
		return workspace.ReviewApproved
	case "CHANGES_REQUESTED":
		return workspace.ReviewChangesRequested
	case "REVIEW_REQUIRED":
		return workspace.ReviewNone
	default:
		return workspace.ReviewNone
	}
}

// rollupToStatus converts statusCheckRollup to a CICheckStatus.
func rollupToStatus(checks []ghCheck) workspace.CICheckStatus {
	if len(checks) == 0 {
		return workspace.CIUnknown
	}
	return aggregateChecks(checks)
}

func aggregateChecks(checks []ghCheck) workspace.CICheckStatus {
	allComplete := true
	for _, c := range checks {
		if mapCheckConclusion(c) == workspace.CIFail {
			return workspace.CIFail
		}
		if !isComplete(c.Status) {
			allComplete = false
		}
	}
	if !allComplete {
		return workspace.CIRunning
	}
	return workspace.CIPass
}

func isComplete(status string) bool {
	return strings.EqualFold(status, "COMPLETED")
}

func mapCheckConclusion(c ghCheck) workspace.CICheckStatus {
	if !isComplete(c.Status) {
		return workspace.CIPending
	}
	switch strings.ToUpper(c.Conclusion) {
	case "SUCCESS", "NEUTRAL", "SKIPPED":
		return workspace.CIPass
	case "FAILURE", "TIMED_OUT", "CANCELLED":
		return workspace.CIFail
	default:
		return workspace.CIUnknown
	}
}

// --- check-runs API parsing -----------------------------------------

type ghCheckRunsResp struct {
	CheckRuns []ghCheckRun `json:"check_runs"`
}

type ghCheckRun struct {
	Name       string       `json:"name"`
	Status     string       `json:"status"`
	Conclusion string       `json:"conclusion"`
	Output     ghCheckOut   `json:"output"`
}

type ghCheckOut struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text"`
}

func parseCheckRunsStatus(output string) (workspace.CICheckStatus, error) {
	var resp ghCheckRunsResp
	if err := parseGHJSON(output, &resp); err != nil {
		return workspace.CIUnknown, fmt.Errorf("parse check runs: %w", err)
	}
	if len(resp.CheckRuns) == 0 {
		return workspace.CIUnknown, nil
	}
	checks := make([]ghCheck, len(resp.CheckRuns))
	for i, cr := range resp.CheckRuns {
		checks[i] = ghCheck{
			Name:       cr.Name,
			Status:     cr.Status,
			Conclusion: cr.Conclusion,
		}
	}
	return aggregateChecks(checks), nil
}

const maxCILogBytes = 8192

func extractFailingLogs(output string) (string, error) {
	var resp ghCheckRunsResp
	if err := parseGHJSON(output, &resp); err != nil {
		return "", fmt.Errorf("parse check runs: %w", err)
	}
	return buildFailingLogSummary(resp.CheckRuns), nil
}

func buildFailingLogSummary(runs []ghCheckRun) string {
	var b strings.Builder
	for _, cr := range runs {
		if !isFailedRun(cr) {
			continue
		}
		appendRunLog(&b, cr)
		if b.Len() >= maxCILogBytes {
			break
		}
	}
	return truncateLog(b.String())
}

func isFailedRun(cr ghCheckRun) bool {
	return isComplete(cr.Status) &&
		strings.EqualFold(cr.Conclusion, "FAILURE")
}

func appendRunLog(b *strings.Builder, cr ghCheckRun) {
	fmt.Fprintf(b, "=== %s ===\n", cr.Name)
	if cr.Output.Summary != "" {
		fmt.Fprintf(b, "%s\n", cr.Output.Summary)
	}
	if cr.Output.Text != "" {
		fmt.Fprintf(b, "%s\n", cr.Output.Text)
	}
}

func truncateLog(s string) string {
	if len(s) > maxCILogBytes {
		return s[:maxCILogBytes] + "\n\n[log truncated]"
	}
	return s
}

// filterPRsByHead filters PR list by head branch prefix.
func filterPRsByHead(
	output string, prefix string,
) ([]*workspace.PR, error) {
	var raws []ghPRRaw
	if err := parseGHJSON(output, &raws); err != nil {
		return nil, fmt.Errorf("parse pr list: %w", err)
	}
	var prs []*workspace.PR
	for i := range raws {
		if strings.HasPrefix(raws[i].HeadRefName, prefix) {
			prs = append(prs, convertRawPR(&raws[i]))
		}
	}
	return prs, nil
}

// --- review parsing -------------------------------------------------

type ghReviewsResp struct {
	Reviews  []ghReview  `json:"reviews"`
	Comments []ghComment `json:"comments"`
}

type ghReview struct {
	ID          int       `json:"id"`
	Author      ghAuthor  `json:"author"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type ghComment struct {
	Author   ghAuthor `json:"author"`
	Body     string   `json:"body"`
	Path     string   `json:"path"`
	Line     int      `json:"line"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

func parseReviewsJSON(output string) ([]*workspace.Review, error) {
	var resp ghReviewsResp
	if err := parseGHJSON(output, &resp); err != nil {
		return nil, fmt.Errorf("parse reviews: %w", err)
	}
	return convertReviews(resp), nil
}

func convertReviews(resp ghReviewsResp) []*workspace.Review {
	reviews := make([]*workspace.Review, 0, len(resp.Reviews))
	for _, r := range resp.Reviews {
		reviews = append(reviews, &workspace.Review{
			ID:          r.ID,
			Author:      r.Author.Login,
			State:       r.State,
			Body:        r.Body,
			SubmittedAt: r.SubmittedAt,
		})
	}
	attachInlineComments(reviews, resp.Comments)
	return reviews
}

func attachInlineComments(
	reviews []*workspace.Review, comments []ghComment,
) {
	for _, c := range comments {
		rc := workspace.ReviewComment{
			Path:   c.Path,
			Line:   c.Line,
			Body:   c.Body,
			Author: c.Author.Login,
		}
		attached := false
		for _, r := range reviews {
			if r.Author == c.Author.Login {
				r.Comments = append(r.Comments, rc)
				attached = true
				break
			}
		}
		if !attached {
			if len(reviews) > 0 {
				reviews[len(reviews)-1].Comments = append(
					reviews[len(reviews)-1].Comments, rc,
				)
			}
			// Orphan comments with no matching review are silently dropped.
			// This is acceptable: inline comments without a review body
			// are rare and the lifecycle manager still sees the review decision.
		}
	}
}
