package daemon

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testWebhookSecret = "whsec_test_secret_123"

// signPayload computes the X-Hub-Signature-256 for a payload.
func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// webhookTestServer creates a Server with a WebhookHandler
// wired in for testing.
func webhookTestServer(t *testing.T) *Server {
	t.Helper()
	s := testServer(t)
	wh := NewWebhookHandler(s.store, testWebhookSecret, s.logger)
	s.mux.HandleFunc("POST /webhooks/github", wh.HandleWebhook)
	return s
}

// sendWebhook sends a signed webhook request and returns the recorder.
func sendWebhook(
	t *testing.T, s *Server,
	event, deliveryID string, payload []byte,
) *httptest.ResponseRecorder {
	t.Helper()
	sig := signPayload(payload, testWebhookSecret)
	req := httptest.NewRequest("POST", "/webhooks/github",
		strings.NewReader(string(payload)))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", deliveryID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestHandleWebhook_LabelTrigger(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{
		"action": "labeled",
		"label": {"name": "altfix"},
		"issue": {
			"number": 42,
			"title": "Fix the login bug",
			"body": "Users cannot log in on mobile"
		},
		"repository": {"full_name": "acme/webapp"}
	}`)

	rec := sendWebhook(t, s, "issues", "delivery-label-1", payload)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tasks, err := s.store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.IssueNumber != 42 {
		t.Errorf("issue_number = %d, want 42", task.IssueNumber)
	}
	if task.RepoOwner != "acme" || task.RepoName != "webapp" {
		t.Errorf("repo = %s/%s, want acme/webapp",
			task.RepoOwner, task.RepoName)
	}
	if !strings.Contains(task.TaskDescription, "Fix the login bug") {
		t.Errorf("task desc missing title: %q", task.TaskDescription)
	}
	if !strings.Contains(task.TaskDescription, "Users cannot log in") {
		t.Errorf("task desc missing body: %q", task.TaskDescription)
	}
	if task.DeliveryID != "delivery-label-1" {
		t.Errorf("delivery_id = %q, want delivery-label-1",
			task.DeliveryID)
	}
}

func TestHandleWebhook_LabelIgnored(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{
		"action": "labeled",
		"label": {"name": "bug"},
		"issue": {
			"number": 10,
			"title": "Some bug",
			"body": ""
		},
		"repository": {"full_name": "acme/webapp"}
	}`)

	rec := sendWebhook(t, s, "issues", "delivery-label-2", payload)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tasks, err := s.store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for non-altfix label, got %d",
			len(tasks))
	}
}

func TestHandleWebhook_CommentTrigger(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{
		"action": "created",
		"comment": {
			"body": "@altfix fix the null pointer in auth.go",
			"user": {"login": "devuser", "type": "User"}
		},
		"issue": {"number": 55, "title": "NPE in auth"},
		"repository": {"full_name": "acme/api"}
	}`)

	rec := sendWebhook(t, s, "issue_comment", "delivery-comment-1",
		payload)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tasks, err := s.store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.IssueNumber != 55 {
		t.Errorf("issue_number = %d, want 55", task.IssueNumber)
	}
	want := "fix the null pointer in auth.go"
	if task.TaskDescription != want {
		t.Errorf("task = %q, want %q", task.TaskDescription, want)
	}
}

func TestHandleWebhook_CommentInCodeBlock(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{
		"action": "created",
		"comment": {
			"body": "Here is an example:\n` + "```" + `\n@altfix do something\n` + "```" + `\nDone.",
			"user": {"login": "devuser", "type": "User"}
		},
		"issue": {"number": 60, "title": "example"},
		"repository": {"full_name": "acme/lib"}
	}`)

	rec := sendWebhook(t, s, "issue_comment", "delivery-code-1",
		payload)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tasks, err := s.store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for @altfix in code block, got %d",
			len(tasks))
	}
}

func TestHandleWebhook_CommentFromBot(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{
		"action": "created",
		"comment": {
			"body": "@altfix auto-fix this",
			"user": {"login": "github-actions", "type": "Bot"}
		},
		"issue": {"number": 70, "title": "bot comment"},
		"repository": {"full_name": "acme/ops"}
	}`)

	rec := sendWebhook(t, s, "issue_comment", "delivery-bot-1",
		payload)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tasks, err := s.store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for Bot user type, got %d",
			len(tasks))
	}
}

func TestHandleWebhook_CommentSelfLoop(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{
		"action": "created",
		"comment": {
			"body": "@altfix loop",
			"user": {"login": "altfix[bot]", "type": "User"}
		},
		"issue": {"number": 80, "title": "self-loop"},
		"repository": {"full_name": "acme/daemon"}
	}`)

	rec := sendWebhook(t, s, "issue_comment", "delivery-loop-1",
		payload)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	tasks, err := s.store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks for [bot] login, got %d",
			len(tasks))
	}
}

func TestHandleWebhook_BadSignature(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{"action":"labeled"}`)
	req := httptest.NewRequest("POST", "/webhooks/github",
		strings.NewReader(string(payload)))
	req.Header.Set("X-Hub-Signature-256", "sha256=bogus")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "delivery-bad-sig")
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Errorf("expected 401 for bad signature, got %d: %s",
			rec.Code, rec.Body.String())
	}
}

func TestHandleWebhook_DuplicateDelivery(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{
		"action": "labeled",
		"label": {"name": "altfix"},
		"issue": {
			"number": 99,
			"title": "Dupe test",
			"body": ""
		},
		"repository": {"full_name": "acme/dupe"}
	}`)

	// First delivery succeeds.
	rec1 := sendWebhook(t, s, "issues", "delivery-dupe-1", payload)
	if rec1.Code != 200 {
		t.Fatalf("first: expected 200, got %d: %s",
			rec1.Code, rec1.Body.String())
	}

	// Same delivery ID again -- should be 409.
	rec2 := sendWebhook(t, s, "issues", "delivery-dupe-1", payload)
	if rec2.Code != 409 {
		t.Errorf("duplicate: expected 409, got %d: %s",
			rec2.Code, rec2.Body.String())
	}

	// Only one task should exist.
	tasks, err := s.store.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after duplicate, got %d", len(tasks))
	}
}

func TestStripCodeAndQuotes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes fenced code",
			input: "before\n```\n@altfix inside\n```\nafter",
			want:  "before\n\nafter",
		},
		{
			name:  "removes blockquotes",
			input: "line1\n> quoted line\nline2",
			want:  "line1\nline2",
		},
		{
			name: "removes both",
			input: "start\n```go\ncode\n```\n> quote\nend",
			want:  "start\n\nend",
		},
		{
			name:  "preserves normal lines",
			input: "hello\nworld",
			want:  "hello\nworld",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripCodeAndQuotes(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractAltfixMention(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "start of line",
			input: "@altfix fix the bug in main.go",
			want:  "fix the bug in main.go",
		},
		{
			name:  "mid-sentence ignored",
			input: "please ask @altfix to help",
			want:  "",
		},
		{
			name:  "in blockquote ignored",
			input: "> @altfix do this",
			want:  "",
		},
		{
			name:  "in code block ignored",
			input: "```\n@altfix fix\n```",
			want:  "",
		},
		{
			name:  "multiline picks first",
			input: "hello\n@altfix refactor auth\nbye",
			want:  "refactor auth",
		},
		{
			name:  "no mention",
			input: "just a regular comment",
			want:  "",
		},
		{
			name:  "altfix alone no instruction",
			input: "@altfix",
			want:  "",
		},
		{
			name:  "altfix with only whitespace after",
			input: "@altfix   ",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractAltfixMention(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleWebhook_UnknownEvent(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{}`)
	rec := sendWebhook(t, s, "push", "delivery-push-1", payload)

	if rec.Code != 200 {
		t.Errorf("expected 200 for unknown event, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "ignored" {
		t.Errorf("expected ignored status, got %v", body)
	}
}

func TestHandleWebhook_MissingDeliveryID(t *testing.T) {
	s := webhookTestServer(t)

	payload := []byte(`{}`)
	sig := signPayload(payload, testWebhookSecret)
	req := httptest.NewRequest("POST", "/webhooks/github",
		strings.NewReader(string(payload)))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "issues")
	// No X-GitHub-Delivery header.
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s",
			rec.Code, rec.Body.String())
	}
}
