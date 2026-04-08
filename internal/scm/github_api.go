package scm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// GitHubAPISCM implements workspace.SCM using direct HTTP calls.
type GitHubAPISCM struct {
	client  *http.Client
	token   string
	owner   string
	repo    string
	baseURL string
}

// NewGitHubAPISCM creates a GitHubAPISCM with the given owner/repo.
func NewGitHubAPISCM(
	owner, repo string,
) (*GitHubAPISCM, error) {
	token := DiscoverGitHubToken()
	if token == "" {
		return nil, fmt.Errorf(
			"github api: no token found; set GITHUB_TOKEN or GH_TOKEN")
	}
	base := os.Getenv("GITHUB_API_URL")
	if base == "" {
		base = "https://api.github.com"
	}
	return &GitHubAPISCM{
		client:  &http.Client{Timeout: 30 * time.Second},
		token:   token,
		owner:   owner,
		repo:    repo,
		baseURL: strings.TrimRight(base, "/"),
	}, nil
}

func (g *GitHubAPISCM) Name() string { return "github-api" }

// DiscoverGitHubToken returns a token using priority:
// 1. GITHUB_TOKEN  2. GH_TOKEN  3. `gh auth token` output
func DiscoverGitHubToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t
		}
	}
	return ""
}

// DetectOwnerRepo parses owner/repo from git remote "origin".
func DetectOwnerRepo() (string, string, error) {
	out, err := exec.Command(
		"git", "remote", "get-url", "origin",
	).Output()
	if err != nil {
		return "", "", fmt.Errorf("detect owner/repo: %w", err)
	}
	return parseRemoteURL(strings.TrimSpace(string(out)))
}

// parseRemoteURL extracts owner/repo from SSH or HTTPS URLs.
func parseRemoteURL(raw string) (string, string, error) {
	// SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(raw, "git@") {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid ssh remote: %s", raw)
		}
		return splitOwnerRepo(parts[1])
	}
	// HTTPS: https://github.com/owner/repo.git
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	segments := strings.Split(raw, "/")
	if len(segments) < 3 {
		return "", "", fmt.Errorf("invalid https remote: %s", raw)
	}
	return splitOwnerRepo(
		segments[len(segments)-2] + "/" + segments[len(segments)-1])
}

func splitOwnerRepo(path string) (string, string, error) {
	path = strings.TrimSuffix(path, ".git")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse owner/repo from %q", path)
	}
	return parts[0], parts[1], nil
}

// --- HTTP helpers ---------------------------------------------------

func (g *GitHubAPISCM) doRequest(
	ctx context.Context,
	method, path string,
	body any,
) (*http.Response, error) {
	url := g.baseURL + "/" + strings.TrimLeft(path, "/")

	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		r = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}

	// Rate-limit awareness: sleep briefly when running low.
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if n, perr := strconv.Atoi(remaining); perr == nil && n < 10 {
			time.Sleep(2 * time.Second)
		}
	}

	return resp, nil
}

func (g *GitHubAPISCM) doJSON(
	ctx context.Context,
	method, path string,
	body, target any,
) error {
	resp, err := g.doRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf(
			"github api %s %s: %d %s",
			method, path, resp.StatusCode, string(data))
	}

	if target != nil {
		if err := json.Unmarshal(data, target); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}

func (g *GitHubAPISCM) repoPath(
	suffix string, args ...any,
) string {
	base := fmt.Sprintf("repos/%s/%s/", g.owner, g.repo)
	return base + fmt.Sprintf(suffix, args...)
}

// --- SCM interface methods ------------------------------------------

// CreatePR creates a pull request via POST /repos/{owner}/{repo}/pulls.
func (g *GitHubAPISCM) CreatePR(
	ctx context.Context, req workspace.CreatePRRequest,
) (*workspace.PR, error) {
	body := apiCreatePR{
		Title: req.Title,
		Body:  req.Body,
		Head:  req.Head,
		Base:  req.Base,
		Draft: req.Draft,
	}
	var raw apiPR
	err := g.doJSON(ctx, http.MethodPost,
		g.repoPath("pulls"), body, &raw)
	if err != nil {
		return nil, fmt.Errorf("create pr: %w", err)
	}
	pr := raw.toWorkspacePR()

	// Apply labels if requested (separate endpoint).
	if len(req.Labels) > 0 {
		labelsBody := map[string][]string{"labels": req.Labels}
		_ = g.doJSON(ctx, http.MethodPost,
			g.repoPath("issues/%d/labels", raw.Number),
			labelsBody, nil)
	}

	return pr, nil
}

// GetPR fetches a PR by number with check-runs and reviews.
func (g *GitHubAPISCM) GetPR(
	ctx context.Context, id int,
) (*workspace.PR, error) {
	var raw apiPR
	err := g.doJSON(ctx, http.MethodGet,
		g.repoPath("pulls/%d", id), nil, &raw)
	if err != nil {
		return nil, fmt.Errorf("get pr %d: %w", id, err)
	}
	pr := raw.toWorkspacePR()

	// Enrich with CI status from check-runs.
	ci, _ := g.CIStatus(ctx, raw.Head.SHA)
	pr.CIStatus = ci

	// Enrich with review status.
	reviews, _ := g.GetPRReviews(ctx, id)
	pr.ReviewStatus = aggregateReviewStatus(reviews)

	return pr, nil
}

// ListPRs returns open PRs whose head branch starts with headPrefix.
func (g *GitHubAPISCM) ListPRs(
	ctx context.Context, headPrefix string,
) ([]*workspace.PR, error) {
	var raws []apiPR
	err := g.doJSON(ctx, http.MethodGet,
		g.repoPath("pulls?state=open&per_page=100"),
		nil, &raws)
	if err != nil {
		return nil, fmt.Errorf("list prs: %w", err)
	}
	var prs []*workspace.PR
	for i := range raws {
		if strings.HasPrefix(raws[i].Head.Ref, headPrefix) {
			prs = append(prs, raws[i].toWorkspacePR())
		}
	}
	return prs, nil
}

// GetPRReviews returns reviews on a PR.
func (g *GitHubAPISCM) GetPRReviews(
	ctx context.Context, prID int,
) ([]*workspace.Review, error) {
	var raws []apiReview
	err := g.doJSON(ctx, http.MethodGet,
		g.repoPath("pulls/%d/reviews", prID), nil, &raws)
	if err != nil {
		return nil, fmt.Errorf("get reviews %d: %w", prID, err)
	}
	reviews := make([]*workspace.Review, 0, len(raws))
	for _, r := range raws {
		reviews = append(reviews, r.toWorkspaceReview())
	}

	// Fetch review comments (inline).
	var comments []apiReviewComment
	_ = g.doJSON(ctx, http.MethodGet,
		g.repoPath("pulls/%d/comments", prID),
		nil, &comments)
	attachAPIComments(reviews, comments)

	return reviews, nil
}

// RequestReview requests review from the given reviewers.
func (g *GitHubAPISCM) RequestReview(
	ctx context.Context, prID int, reviewers []string,
) error {
	if len(reviewers) == 0 {
		return nil
	}
	body := map[string][]string{"reviewers": reviewers}
	return g.doJSON(ctx, http.MethodPost,
		g.repoPath("pulls/%d/requested_reviewers", prID),
		body, nil)
}

// MergePR merges the PR using the given method.
func (g *GitHubAPISCM) MergePR(
	ctx context.Context, prID int, method workspace.MergeMethod,
) error {
	body := map[string]string{"merge_method": string(method)}
	return g.doJSON(ctx, http.MethodPut,
		g.repoPath("pulls/%d/merge", prID), body, nil)
}

// CIStatus returns the combined CI status for a commit SHA.
func (g *GitHubAPISCM) CIStatus(
	ctx context.Context, sha string,
) (workspace.CICheckStatus, error) {
	var resp ghCheckRunsResp
	err := g.doJSON(ctx, http.MethodGet,
		g.repoPath("commits/%s/check-runs", sha), nil, &resp)
	if err != nil {
		return workspace.CIUnknown, fmt.Errorf("ci status: %w", err)
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

// CILogs fetches combined log output from failing checks (max 8KB).
func (g *GitHubAPISCM) CILogs(
	ctx context.Context, sha string,
) (string, error) {
	var resp ghCheckRunsResp
	err := g.doJSON(ctx, http.MethodGet,
		g.repoPath("commits/%s/check-runs", sha), nil, &resp)
	if err != nil {
		return "", fmt.Errorf("ci logs: %w", err)
	}
	return buildFailingLogSummary(resp.CheckRuns), nil
}

// --- API response types ---------------------------------------------

type apiCreatePR struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
	Draft bool   `json:"draft"`
}

type apiPR struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	HTMLURL   string    `json:"html_url"`
	State     string    `json:"state"`
	Draft     bool      `json:"draft"`
	Mergeable *bool     `json:"mergeable"`
	Head      apiRef    `json:"head"`
	Base      apiRef    `json:"base"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type apiRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

func (r *apiPR) toWorkspacePR() *workspace.PR {
	ms := "unknown"
	if r.Mergeable != nil {
		if *r.Mergeable {
			ms = "MERGEABLE"
		} else {
			ms = "CONFLICTING"
		}
	}
	return &workspace.PR{
		ID:             r.Number,
		Number:         r.Number,
		Title:          r.Title,
		URL:            r.HTMLURL,
		HeadSHA:        r.Head.SHA,
		HeadBranch:     r.Head.Ref,
		BaseBranch:     r.Base.Ref,
		State:          mapPRState(strings.ToUpper(r.State)),
		Draft:          r.Draft,
		MergeableState: ms,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

type apiReview struct {
	ID          int       `json:"id"`
	User        apiUser   `json:"user"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submitted_at"`
}

type apiUser struct {
	Login string `json:"login"`
}

func (r *apiReview) toWorkspaceReview() *workspace.Review {
	return &workspace.Review{
		ID:          r.ID,
		Author:      r.User.Login,
		State:       r.State,
		Body:        r.Body,
		SubmittedAt: r.SubmittedAt,
	}
}

type apiReviewComment struct {
	User apiUser `json:"user"`
	Body string  `json:"body"`
	Path string  `json:"path"`
	Line int     `json:"line"`
}

func attachAPIComments(
	reviews []*workspace.Review, comments []apiReviewComment,
) {
	for _, c := range comments {
		rc := workspace.ReviewComment{
			Path:   c.Path,
			Line:   c.Line,
			Body:   c.Body,
			Author: c.User.Login,
		}
		attached := false
		for _, r := range reviews {
			if r.Author == c.User.Login {
				r.Comments = append(r.Comments, rc)
				attached = true
				break
			}
		}
		if !attached && len(reviews) > 0 {
			last := reviews[len(reviews)-1]
			last.Comments = append(last.Comments, rc)
		}
	}
}

func aggregateReviewStatus(
	reviews []*workspace.Review,
) workspace.ReviewStatus {
	if len(reviews) == 0 {
		return workspace.ReviewNone
	}
	// Last decisive review wins.
	for i := len(reviews) - 1; i >= 0; i-- {
		switch strings.ToUpper(reviews[i].State) {
		case "APPROVED":
			return workspace.ReviewApproved
		case "CHANGES_REQUESTED":
			return workspace.ReviewChangesRequested
		}
	}
	return workspace.ReviewNone
}
