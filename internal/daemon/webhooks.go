package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

// WebhookHandler processes GitHub webhook events.
type WebhookHandler struct {
	store  *Store
	secret string // webhook signing secret
	logger *slog.Logger
}

// NewWebhookHandler creates a handler that verifies signatures
// against secret and persists tasks to store.
func NewWebhookHandler(
	store *Store, secret string, logger *slog.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		store:  store,
		secret: secret,
		logger: logger,
	}
}

// HandleWebhook is the HTTP handler for POST /webhooks/github.
// It verifies the signature, deduplicates via delivery ID, and
// routes by event type.
func (wh *WebhookHandler) HandleWebhook(
	w http.ResponseWriter, r *http.Request,
) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB
	if err != nil {
		http.Error(w, `{"error":"read body failed"}`, 400)
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if !VerifyWebhookSignature(body, sig, wh.secret) {
		http.Error(w, `{"error":"invalid signature"}`, 401)
		return
	}

	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		http.Error(w, `{"error":"missing delivery ID"}`, 400)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")

	var handleErr error
	switch eventType {
	case "issues":
		handleErr = wh.handleIssueEvent(body, deliveryID)
	case "issue_comment":
		handleErr = wh.handleCommentEvent(body, deliveryID)
	case "pull_request_review_comment":
		handleErr = wh.handlePRCommentEvent(body, deliveryID)
	default:
		// Unhandled event type -- acknowledge silently.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"status":"ignored","event":%q}`, eventType)
		return
	}

	if handleErr != nil {
		if isDuplicateDelivery(handleErr) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			fmt.Fprintf(w, `{"error":"duplicate delivery"}`)
			return
		}
		wh.logger.Error("webhook handler error",
			"event", eventType,
			"delivery", deliveryID,
			"err", handleErr,
		)
		http.Error(w, `{"error":"internal error"}`, 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// issuePayload is the subset of GitHub issues webhook we parse.
type issuePayload struct {
	Action string `json:"action"`
	Label  struct {
		Name string `json:"name"`
	} `json:"label"`
	Issue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	} `json:"issue"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// handleIssueEvent processes issues.labeled events.
// Creates a task when the "altfix" label is applied.
func (wh *WebhookHandler) handleIssueEvent(
	payload []byte, deliveryID string,
) error {
	var p issuePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("parse issue payload: %w", err)
	}

	if p.Action != "labeled" || p.Label.Name != "altfix" {
		return nil
	}

	owner, repo := splitFullName(p.Repository.FullName)
	repoURL := "https://github.com/" + p.Repository.FullName

	desc := p.Issue.Title
	if p.Issue.Body != "" {
		desc += "\n\n" + p.Issue.Body
	}

	task := &Task{
		RepoURL:         repoURL,
		TaskDescription: desc,
		Status:          "pending",
		IssueNumber:     p.Issue.Number,
		DeliveryID:      deliveryID,
		RepoOwner:       owner,
		RepoName:        repo,
	}
	return wh.store.CreateTask(task)
}

// commentPayload is the subset of issue_comment / PR review
// comment webhooks we parse.
type commentPayload struct {
	Action  string `json:"action"`
	Comment struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
	} `json:"comment"`
	Issue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"issue"`
	PullRequest *struct {
		Number int `json:"number"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// handleCommentEvent processes issue_comment.created events.
// Creates a task when a human comments with @altfix at the start
// of a line. Rejects bot comments to prevent self-loops.
func (wh *WebhookHandler) handleCommentEvent(
	payload []byte, deliveryID string,
) error {
	var p commentPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("parse comment payload: %w", err)
	}

	if p.Action != "created" {
		return nil
	}

	// Bot guard: reject Bot type and [bot] suffix.
	if p.Comment.User.Type == "Bot" {
		return nil
	}
	if strings.HasSuffix(p.Comment.User.Login, "[bot]") {
		return nil
	}

	instruction := extractAltfixMention(p.Comment.Body)
	if instruction == "" {
		return nil
	}

	owner, repo := splitFullName(p.Repository.FullName)
	repoURL := "https://github.com/" + p.Repository.FullName

	issueNum := p.Issue.Number
	if p.PullRequest != nil && p.PullRequest.Number > 0 {
		issueNum = p.PullRequest.Number
	}

	task := &Task{
		RepoURL:         repoURL,
		TaskDescription: instruction,
		Status:          "pending",
		IssueNumber:     issueNum,
		DeliveryID:      deliveryID,
		RepoOwner:       owner,
		RepoName:        repo,
	}
	return wh.store.CreateTask(task)
}

// handlePRCommentEvent processes pull_request_review_comment
// events. Uses the same logic as issue comments.
func (wh *WebhookHandler) handlePRCommentEvent(
	payload []byte, deliveryID string,
) error {
	return wh.handleCommentEvent(payload, deliveryID)
}

// stripCodeAndQuotes removes fenced code blocks and blockquote
// lines from text before scanning for @altfix mentions.
func stripCodeAndQuotes(text string) string {
	// Remove fenced code blocks (``` ... ```).
	reFence := regexp.MustCompile("(?s)```.*?```")
	text = reFence.ReplaceAllString(text, "")

	// Remove blockquote lines (> ...).
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// extractAltfixMention finds @altfix at the start of a
// non-quoted, non-code line. Returns the instruction text
// after @altfix, or empty string if no match.
func extractAltfixMention(text string) string {
	cleaned := stripCodeAndQuotes(text)
	re := regexp.MustCompile(`(?m)^@altfix\s+(.+)`)
	matches := re.FindStringSubmatch(cleaned)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

// splitFullName splits "owner/repo" into (owner, repo).
func splitFullName(fullName string) (string, string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return fullName, ""
	}
	return parts[0], parts[1]
}

// isDuplicateDelivery checks if a store error indicates a UNIQUE
// constraint violation on delivery_id.
func isDuplicateDelivery(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "UNIQUE") ||
		strings.Contains(msg, "duplicate")
}
