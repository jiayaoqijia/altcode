package scm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

func TestDiscoverGitHubToken(t *testing.T) {
	// GITHUB_TOKEN takes priority.
	t.Setenv("GITHUB_TOKEN", "tok-gh")
	t.Setenv("GH_TOKEN", "tok-cli")
	got := DiscoverGitHubToken()
	assertEqual(t, "GITHUB_TOKEN priority", got, "tok-gh")

	// GH_TOKEN is second.
	t.Setenv("GITHUB_TOKEN", "")
	got = DiscoverGitHubToken()
	assertEqual(t, "GH_TOKEN fallback", got, "tok-cli")
}

func TestDetectOwnerRepoSSH(t *testing.T) {
	owner, repo, err := parseRemoteURL(
		"git@github.com:acme/widgets.git")
	assertErrNil(t, "ssh", err)
	assertEqual(t, "owner", owner, "acme")
	assertEqual(t, "repo", repo, "widgets")
}

func TestDetectOwnerRepoHTTPS(t *testing.T) {
	owner, repo, err := parseRemoteURL(
		"https://github.com/acme/widgets.git")
	assertErrNil(t, "https", err)
	assertEqual(t, "owner", owner, "acme")
	assertEqual(t, "repo", repo, "widgets")
}

func TestDetectOwnerRepoHTTPSNoSuffix(t *testing.T) {
	owner, repo, err := parseRemoteURL(
		"https://github.com/acme/widgets")
	assertErrNil(t, "https-no-git", err)
	assertEqual(t, "owner", owner, "acme")
	assertEqual(t, "repo", repo, "widgets")
}

func TestCreatePR_API(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost &&
				r.URL.Path == "/repos/acme/widgets/pulls":
				resp := apiPR{
					Number:    7,
					Title:     "feat: add X",
					HTMLURL:   "https://github.com/acme/widgets/pull/7",
					State:     "open",
					Draft:     true,
					Head:      apiRef{Ref: "feat-x", SHA: "abc"},
					Base:      apiRef{Ref: "main", SHA: "def"},
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(resp)
			default:
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("{}"))
			}
		}))
	defer srv.Close()

	g := &GitHubAPISCM{
		client:  srv.Client(),
		token:   "test-token",
		owner:   "acme",
		repo:    "widgets",
		baseURL: srv.URL,
	}

	pr, err := g.CreatePR(context.Background(),
		workspace.CreatePRRequest{
			Title: "feat: add X",
			Body:  "desc",
			Head:  "feat-x",
			Base:  "main",
			Draft: true,
		})
	assertErrNil(t, "CreatePR", err)
	assertEqual(t, "number", pr.Number, 7)
	assertEqual(t, "title", pr.Title, "feat: add X")
	assertEqual(t, "state", pr.State, "open")
	assertEqual(t, "draft", pr.Draft, true)
}

func TestCIStatus_API(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			resp := ghCheckRunsResp{
				CheckRuns: []ghCheckRun{
					{
						Name:       "build",
						Status:     "completed",
						Conclusion: "success",
					},
					{
						Name:       "lint",
						Status:     "completed",
						Conclusion: "success",
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		}))
	defer srv.Close()

	g := &GitHubAPISCM{
		client:  srv.Client(),
		token:   "test-token",
		owner:   "acme",
		repo:    "widgets",
		baseURL: srv.URL,
	}

	// Use a valid 7+ char hex SHA — validateSHA rejects shorter values
	// to defend against injection through the URL path.
	status, err := g.CIStatus(
		context.Background(), "abc1234")
	assertErrNil(t, "CIStatus", err)
	assertEqual(t, "status", status, workspace.CIPass)
}

func TestGetPR_API(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/acme/widgets/pulls/42":
				resp := apiPR{
					Number:  42,
					Title:   "fix: bug",
					HTMLURL: "https://github.com/acme/widgets/pull/42",
					State:   "open",
					// Realistic 7+ char hex SHAs so validateSHA accepts
					// them — the previous "sha1" placeholder was 4
					// characters and tripped the new validation guard.
					Head: apiRef{Ref: "fix-bug", SHA: "abc1234"},
					Base: apiRef{Ref: "main", SHA: "def5678"},
				}
				json.NewEncoder(w).Encode(resp)
			case "/repos/acme/widgets/commits/abc1234/check-runs":
				resp := ghCheckRunsResp{
					CheckRuns: []ghCheckRun{
						{
							Name:       "ci",
							Status:     "completed",
							Conclusion: "failure",
						},
					},
				}
				json.NewEncoder(w).Encode(resp)
			case "/repos/acme/widgets/pulls/42/reviews":
				w.Write([]byte("[]"))
			default:
				w.Write([]byte("{}"))
			}
		}))
	defer srv.Close()

	g := &GitHubAPISCM{
		client:  srv.Client(),
		token:   "test-token",
		owner:   "acme",
		repo:    "widgets",
		baseURL: srv.URL,
	}

	pr, err := g.GetPR(context.Background(), 42)
	assertErrNil(t, "GetPR", err)
	assertEqual(t, "number", pr.Number, 42)
	assertEqual(t, "head", pr.HeadBranch, "fix-bug")
	assertEqual(t, "ci", pr.CIStatus, workspace.CIFail)
}

func TestAggregateReviewStatus(t *testing.T) {
	tests := []struct {
		name    string
		reviews []*workspace.Review
		want    workspace.ReviewStatus
	}{
		{"empty", nil, workspace.ReviewNone},
		{
			"approved",
			[]*workspace.Review{{State: "APPROVED"}},
			workspace.ReviewApproved,
		},
		{
			"changes_requested",
			[]*workspace.Review{{State: "CHANGES_REQUESTED"}},
			workspace.ReviewChangesRequested,
		},
		{
			"last_wins",
			[]*workspace.Review{
				{State: "CHANGES_REQUESTED"},
				{State: "APPROVED"},
			},
			workspace.ReviewApproved,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := aggregateReviewStatus(tc.reviews)
			assertEqual(t, "status", got, tc.want)
		})
	}
}

func TestParseRemoteURLInvalid(t *testing.T) {
	_, _, err := parseRemoteURL("not-a-url")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestGitHubAPISCMName(t *testing.T) {
	g := &GitHubAPISCM{}
	assertEqual(t, "name", g.Name(), "github-api")
}
