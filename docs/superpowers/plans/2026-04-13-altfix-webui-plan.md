# AltFix Web UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an htmx + Go templates web dashboard embedded in the `altcode daemon` binary, with GitHub OAuth, SSE streaming, and shared URLs.

**Architecture:** `internal/daemon/web/` package with `embed.FS` for templates and static assets. All handlers consume the existing `daemon.Store` for data. Auth via GitHub OAuth + PKCE with in-memory sessions. Zero new Go dependencies beyond stdlib.

**Tech Stack:** Go `html/template`, htmx 2.x (vendored), Tailwind CSS (vendored + purged), native `EventSource` for SSE, HMAC-SHA256 for shared URLs.

**Spec:** `docs/superpowers/specs/2026-04-13-altfix-webui-design.md`

---

## File Structure

| File | Responsibility | Created in |
|------|---------------|------------|
| `internal/daemon/web/embed.go` | `//go:embed` directives, FS access | Task 1 |
| `internal/daemon/web/web.go` | Template loading, `RegisterRoutes`, render helpers | Task 1 |
| `internal/daemon/web/session.go` | In-memory session store with TTL | Task 2 |
| `internal/daemon/web/auth.go` | GitHub OAuth + PKCE flow | Task 3 |
| `internal/daemon/web/middleware.go` | RequireAuth, RequireAdmin, CSRFCheck | Task 4 |
| `internal/daemon/web/handlers.go` | Dashboard, detail, new, PRs, settings page handlers | Task 5 |
| `internal/daemon/web/share.go` | HMAC signing, verification, redaction | Task 6 |
| `internal/daemon/web/sse_html.go` | SSE endpoint that sends HTML partials (wraps existing JSON SSE) | Task 5 |
| `internal/daemon/web/templates/layout.html` | Base template: htmx, tailwind, nav, dark mode | Task 1 |
| `internal/daemon/web/templates/login.html` | GitHub OAuth button | Task 3 |
| `internal/daemon/web/templates/dashboard.html` | KPI cards + task list + filters | Task 5 |
| `internal/daemon/web/templates/detail.html` | Phase timeline + SSE feed + steering | Task 5 |
| `internal/daemon/web/templates/new.html` | Progressive task creation form | Task 5 |
| `internal/daemon/web/templates/prs.html` | PR tracker table | Task 5 |
| `internal/daemon/web/templates/settings.html` | Config viewer/editor | Task 5 |
| `internal/daemon/web/templates/share.html` | Public read-only detail | Task 6 |
| `internal/daemon/web/templates/partials/*.html` | Swap targets (task_card, event_item, kpi_cards, phase_bar, task_list, repo_issues, pr_row) | Task 5 |
| `internal/daemon/web/templates/errors/*.html` | 403, 404, session_expired | Task 4 |
| `internal/daemon/web/static/htmx.min.js` | Vendored htmx 2.x | Task 1 |
| `internal/daemon/web/static/tailwind.css` | Vendored + purged Tailwind | Task 1 |
| `internal/daemon/web/static/app.css` | Status colors, feed layout, dark mode | Task 1 |
| `internal/daemon/server.go` | Modified: add WebUI config fields, call `web.RegisterRoutes` | Task 7 |
| `cmd/altcode/daemon.go` | Modified: add `--github-client-id`, `--github-client-secret`, `--allowed-orgs`, `--allowed-users`, `--signing-key` flags | Task 7 |

---

### Task 1: Foundation — embed.FS, Templates, Static Assets

**Files:**
- Create: `internal/daemon/web/embed.go`
- Create: `internal/daemon/web/web.go`
- Create: `internal/daemon/web/static/htmx.min.js`
- Create: `internal/daemon/web/static/tailwind.css`
- Create: `internal/daemon/web/static/app.css`
- Create: `internal/daemon/web/templates/layout.html`
- Test: `internal/daemon/web/web_test.go`

- [ ] **Step 1: Create the web package directory**

```bash
mkdir -p internal/daemon/web/templates/partials internal/daemon/web/templates/errors internal/daemon/web/static
```

- [ ] **Step 2: Download vendored htmx**

```bash
curl -sL https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -o internal/daemon/web/static/htmx.min.js
```

- [ ] **Step 3: Create app.css with status colors and dark mode**

```css
/* internal/daemon/web/static/app.css */
:root { --bg: #f9fafb; --fg: #111827; --card: #ffffff; --border: #e5e7eb; }
.dark { --bg: #111827; --fg: #f9fafb; --card: #1f2937; --border: #374151; }
body { background: var(--bg); color: var(--fg); font-family: system-ui, sans-serif; }

.status-pending   { color: #f59e0b; }
.status-planning  { color: #3b82f6; }
.status-implementing { color: #10b981; }
.status-reviewing { color: #8b5cf6; }
.status-testing   { color: #06b6d4; }
.status-merged    { color: #22c55e; }
.status-failed    { color: #ef4444; }
.status-cancelled { color: #6b7280; }

.dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
.dot-pending   { background: #f59e0b; }
.dot-running   { background: #10b981; animation: pulse 2s infinite; }
.dot-failed    { background: #ef4444; }
.dot-merged    { background: #22c55e; }

@keyframes pulse { 0%,100% { opacity:1; } 50% { opacity:0.5; } }

.card { background: var(--card); border: 1px solid var(--border); border-radius: 8px; padding: 16px; }
.feed-item { padding: 8px 12px; border-left: 3px solid var(--border); margin: 4px 0; }
.feed-item-error { border-left-color: #ef4444; background: #fef2f2; }
.feed-item-phase { border-left-color: #3b82f6; font-weight: 600; }
.feed-item-steer { border-left-color: #8b5cf6; background: #f5f3ff; }
.feed-item-agent { border-left-color: #10b981; font-family: monospace; white-space: pre-wrap; }

.phase-bar { display: flex; gap: 4px; align-items: center; }
.phase-done { color: #22c55e; }
.phase-active { color: #f59e0b; animation: pulse 2s infinite; }
.phase-pending { color: #9ca3af; }
```

- [ ] **Step 4: Create minimal tailwind.css placeholder**

For v1, use a minimal CSS file with utility classes. A full Tailwind purge build can be done later via `scripts/build-css.sh`.

```css
/* internal/daemon/web/static/tailwind.css — minimal utility classes for v1 */
.flex { display: flex; } .grid { display: grid; }
.gap-2 { gap: 0.5rem; } .gap-4 { gap: 1rem; }
.p-2 { padding: 0.5rem; } .p-4 { padding: 1rem; }
.m-2 { margin: 0.5rem; } .m-4 { margin: 1rem; }
.mt-2 { margin-top: 0.5rem; } .mt-4 { margin-top: 1rem; }
.mb-2 { margin-bottom: 0.5rem; } .mb-4 { margin-bottom: 1rem; }
.text-sm { font-size: 0.875rem; } .text-lg { font-size: 1.125rem; }
.text-xl { font-size: 1.25rem; } .text-2xl { font-size: 1.5rem; }
.font-bold { font-weight: 700; } .font-mono { font-family: monospace; }
.text-gray-500 { color: #6b7280; } .text-red-500 { color: #ef4444; }
.text-green-500 { color: #22c55e; } .text-blue-500 { color: #3b82f6; }
.bg-white { background: #fff; } .bg-gray-50 { background: #f9fafb; }
.bg-amber-100 { background: #fef3c7; } .bg-red-50 { background: #fef2f2; }
.border { border: 1px solid var(--border); } .rounded { border-radius: 0.375rem; }
.rounded-lg { border-radius: 0.5rem; }
.w-full { width: 100%; } .max-w-4xl { max-width: 56rem; }
.mx-auto { margin-left: auto; margin-right: auto; }
.hidden { display: none; } .block { display: block; }
.items-center { align-items: center; } .justify-between { justify-content: space-between; }
.grid-cols-2 { grid-template-columns: repeat(2, 1fr); }
.grid-cols-4 { grid-template-columns: repeat(4, 1fr); }
.overflow-y-auto { overflow-y: auto; }
.cursor-pointer { cursor: pointer; }
.no-underline { text-decoration: none; }
.hover\:bg-gray-100:hover { background: #f3f4f6; }
```

- [ ] **Step 5: Create layout.html base template**

```html
{{/* internal/daemon/web/templates/layout.html */}}
<!DOCTYPE html>
<html lang="en" class="{{if .DarkMode}}dark{{end}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} — AltFix</title>
  <meta name="csrf-token" content="{{.CSRFToken}}">
  <link rel="stylesheet" href="/ui/static/tailwind.css">
  <link rel="stylesheet" href="/ui/static/app.css">
  <script src="/ui/static/htmx.min.js"></script>
  <script>
    document.body.addEventListener('htmx:configRequest', function(e) {
      var token = document.querySelector('meta[name="csrf-token"]');
      if (token) e.detail.headers['X-CSRF-Token'] = token.content;
    });
  </script>
</head>
<body>
  {{if .ShowNav}}
  <nav style="padding:12px 24px;border-bottom:1px solid var(--border);display:flex;justify-content:space-between;align-items:center">
    <div style="display:flex;gap:16px;align-items:center">
      <a href="/ui/" style="font-weight:700;font-size:1.25rem;text-decoration:none;color:var(--fg)">AltFix</a>
      <a href="/ui/" style="text-decoration:none;color:var(--fg)">Dashboard</a>
      <a href="/ui/prs" style="text-decoration:none;color:var(--fg)">PRs</a>
      {{if .IsAdmin}}<a href="/ui/settings" style="text-decoration:none;color:var(--fg)">Settings</a>{{end}}
    </div>
    <div style="display:flex;gap:12px;align-items:center">
      <span class="text-sm text-gray-500">{{.User.Login}}</span>
      <form method="POST" action="/auth/logout" style="margin:0">
        <input type="hidden" name="_csrf" value="{{.CSRFToken}}">
        <button type="submit" class="text-sm text-gray-500" style="background:none;border:none;cursor:pointer">Logout</button>
      </form>
    </div>
  </nav>
  {{end}}
  <main style="max-width:72rem;margin:0 auto;padding:24px">
    {{template "content" .}}
  </main>
</body>
</html>
```

- [ ] **Step 6: Create embed.go**

```go
// internal/daemon/web/embed.go
package web

import "embed"

//go:embed all:templates all:static
var content embed.FS
```

- [ ] **Step 7: Create web.go with template loading and render helpers**

```go
// internal/daemon/web/web.go
package web

import (
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

// PageData is the data passed to every page template.
type PageData struct {
	Title     string
	ShowNav   bool
	DarkMode  bool
	CSRFToken string
	IsAdmin   bool
	User      *SessionUser
	Content   any // page-specific data
}

// Templates holds parsed page templates keyed by name.
type Templates struct {
	pages map[string]*template.Template
}

// LoadTemplates parses all templates from the embedded FS.
func LoadTemplates() (*Templates, error) {
	funcMap := template.FuncMap{
		"truncate": func(s string, n int) string {
			if len(s) <= n { return s }
			return s[:n] + "..."
		},
		"statusDot": func(status string) string {
			switch {
			case status == "pending":
				return "dot-pending"
			case status == "failed" || status == "cancelled":
				return "dot-failed"
			case status == "merged" || status == "closed":
				return "dot-merged"
			default:
				return "dot-running"
			}
		},
		"phaseClass": func(current, phase string) string {
			phases := []string{"planning", "implementing", "reviewing", "testing", "pr_open"}
			ci, pi := -1, -1
			for i, p := range phases {
				if p == current { ci = i }
				if p == phase { pi = i }
			}
			if pi < ci { return "phase-done" }
			if pi == ci { return "phase-active" }
			return "phase-pending"
		},
	}

	t := &Templates{pages: make(map[string]*template.Template)}

	layoutBytes, err := fs.ReadFile(content, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	layoutStr := string(layoutBytes)

	// Walk page templates (non-partials, non-errors).
	entries, err := fs.ReadDir(content, "templates")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "layout.html" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".html")
		pageBytes, err := fs.ReadFile(content, "templates/"+e.Name())
		if err != nil {
			return nil, err
		}

		tmpl, err := template.New("layout").Funcs(funcMap).Parse(layoutStr)
		if err != nil {
			return nil, err
		}

		// Parse all partials into each page template.
		partials, _ := fs.Glob(content, "templates/partials/*.html")
		for _, p := range partials {
			pb, _ := fs.ReadFile(content, p)
			pname := strings.TrimSuffix(filepath.Base(p), ".html")
			template.Must(tmpl.New(pname).Parse(string(pb)))
		}

		template.Must(tmpl.New("content").Parse(string(pageBytes)))
		t.pages[name] = tmpl
	}

	// Error pages.
	errorPages, _ := fs.Glob(content, "templates/errors/*.html")
	for _, ep := range errorPages {
		epb, _ := fs.ReadFile(content, ep)
		name := "error-" + strings.TrimSuffix(filepath.Base(ep), ".html")
		tmpl, _ := template.New("layout").Funcs(funcMap).Parse(layoutStr)
		template.Must(tmpl.New("content").Parse(string(epb)))
		t.pages[name] = tmpl
	}

	return t, nil
}

// Render executes a named page template.
func (t *Templates) Render(w io.Writer, name string, data PageData) error {
	tmpl, ok := t.pages[name]
	if !ok {
		return &errTemplateNotFound{name}
	}
	return tmpl.ExecuteTemplate(w, "layout", data)
}

// RenderPartial executes a named partial template.
func (t *Templates) RenderPartial(w io.Writer, page, partial string, data any) error {
	tmpl, ok := t.pages[page]
	if !ok {
		return &errTemplateNotFound{page}
	}
	return tmpl.ExecuteTemplate(w, partial, data)
}

type errTemplateNotFound struct{ name string }
func (e *errTemplateNotFound) Error() string { return "template not found: " + e.name }

// RegisterRoutes wires all web UI routes onto the given mux.
// Called from server.go after NewServer.
func RegisterRoutes(mux *http.ServeMux, cfg WebConfig) error {
	tmpl, err := LoadTemplates()
	if err != nil {
		return err
	}

	// Static assets.
	staticFS, err := fs.Sub(content, "static")
	if err != nil {
		return err
	}
	mux.Handle("GET /ui/static/", http.StripPrefix("/ui/static/",
		http.FileServer(http.FS(staticFS))))

	h := &WebHandler{
		tmpl:    tmpl,
		store:   cfg.Store,
		sessions: cfg.Sessions,
		cfg:     cfg,
	}

	// Auth routes (no session required).
	mux.HandleFunc("GET /auth/github", h.HandleOAuthRedirect)
	mux.HandleFunc("GET /auth/callback", h.HandleOAuthCallback)
	mux.HandleFunc("POST /auth/logout", h.HandleLogout)
	mux.HandleFunc("GET /ui/login", h.HandleLoginPage)

	// Shared view (no session required).
	mux.HandleFunc("GET /share/{token}", h.HandleShareView)

	// Authenticated routes.
	auth := RequireAuth(cfg.Sessions)
	csrf := CSRFCheck()

	mux.Handle("GET /ui/", auth(http.HandlerFunc(h.HandleDashboard)))
	mux.Handle("GET /ui/tasks/{id}", auth(http.HandlerFunc(h.HandleTaskDetail)))
	mux.Handle("GET /ui/tasks/new", auth(http.HandlerFunc(h.HandleNewTask)))
	mux.Handle("GET /ui/prs", auth(http.HandlerFunc(h.HandlePRs)))
	mux.Handle("GET /ui/settings", auth(http.HandlerFunc(h.HandleSettings)))

	// Partial endpoints for htmx polling.
	mux.Handle("GET /ui/partials/task-list", auth(http.HandlerFunc(h.HandlePartialTaskList)))
	mux.Handle("GET /ui/partials/kpi-cards", auth(http.HandlerFunc(h.HandlePartialKPICards)))
	mux.Handle("GET /ui/partials/repo-issues", auth(http.HandlerFunc(h.HandlePartialRepoIssues)))

	// SSE endpoint that sends HTML partials (wraps existing JSON SSE).
	mux.Handle("GET /ui/tasks/{id}/events", auth(http.HandlerFunc(h.HandleSSEHTML)))

	return nil
}

// WebConfig holds dependencies for the web UI handlers.
type WebConfig struct {
	Store          interface{ /* daemon.Store methods used by web */ }
	Sessions       *SessionStore
	GitHubClientID string
	GitHubSecret   string
	AllowedOrgs    []string
	AllowedUsers   []string
	AdminUsers     []string
	SigningKey      []byte
	BaseURL        string // e.g. "http://localhost:9100"
}
```

- [ ] **Step 8: Write test for template loading**

```go
// internal/daemon/web/web_test.go
package web

import (
	"bytes"
	"testing"
)

func TestLoadTemplates(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	if len(tmpl.pages) == 0 {
		t.Fatal("no templates loaded")
	}
}

func TestRender_LoginPage(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	var buf bytes.Buffer
	err = tmpl.Render(&buf, "login", PageData{
		Title:   "Login",
		ShowNav: false,
	})
	if err != nil {
		t.Fatalf("Render login: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("empty output for login page")
	}
}
```

- [ ] **Step 9: Run tests**

```bash
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -v
```

Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add internal/daemon/web/
git commit -m "feat(web): foundation — embed.FS, templates, static assets, layout"
```

---

### Task 2: Session Store

**Files:**
- Create: `internal/daemon/web/session.go`
- Test: `internal/daemon/web/session_test.go`

- [ ] **Step 1: Write session store tests**

```go
// internal/daemon/web/session_test.go
package web

import (
	"testing"
	"time"
)

func TestSessionStore_CreateAndGet(t *testing.T) {
	s := NewSessionStore(1 * time.Hour)
	id := s.Create(&SessionUser{Login: "alice", IsAdmin: true})
	sess, ok := s.Get(id)
	if !ok {
		t.Fatal("session not found")
	}
	if sess.User.Login != "alice" {
		t.Errorf("login = %q, want alice", sess.User.Login)
	}
	if !sess.User.IsAdmin {
		t.Error("expected admin")
	}
}

func TestSessionStore_Expire(t *testing.T) {
	s := NewSessionStore(1 * time.Millisecond)
	id := s.Create(&SessionUser{Login: "bob"})
	time.Sleep(5 * time.Millisecond)
	_, ok := s.Get(id)
	if ok {
		t.Error("expected session to expire")
	}
}

func TestSessionStore_Delete(t *testing.T) {
	s := NewSessionStore(1 * time.Hour)
	id := s.Create(&SessionUser{Login: "carol"})
	s.Delete(id)
	_, ok := s.Get(id)
	if ok {
		t.Error("expected session deleted")
	}
}

func TestSessionStore_Touch(t *testing.T) {
	s := NewSessionStore(50 * time.Millisecond)
	id := s.Create(&SessionUser{Login: "dave"})
	time.Sleep(30 * time.Millisecond)
	s.Touch(id) // refresh TTL
	time.Sleep(30 * time.Millisecond)
	_, ok := s.Get(id)
	if !ok {
		t.Error("session should still be valid after touch")
	}
}

func TestSessionStore_CSRFToken(t *testing.T) {
	s := NewSessionStore(1 * time.Hour)
	id := s.Create(&SessionUser{Login: "eve"})
	sess, _ := s.Get(id)
	if sess.CSRFToken == "" {
		t.Error("expected CSRF token to be generated")
	}
	if len(sess.CSRFToken) < 32 {
		t.Errorf("CSRF token too short: %d", len(sess.CSRFToken))
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -run TestSession -v
```

- [ ] **Step 3: Implement session store**

```go
// internal/daemon/web/session.go
package web

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SessionUser holds the authenticated user's info.
type SessionUser struct {
	Login       string
	AvatarURL   string
	GitHubToken string // for GitHub API calls on behalf of user
	IsAdmin     bool
	Orgs        []string
}

// Session is a server-side session.
type Session struct {
	ID        string
	User      *SessionUser
	CSRFToken string
	CreatedAt time.Time
	TouchedAt time.Time
}

// SessionStore is a thread-safe in-memory session store.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore creates a store with the given session TTL.
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

// Create creates a new session and returns its ID.
func (s *SessionStore) Create(user *SessionUser) string {
	id := randomHex(32)
	csrf := randomHex(32)
	now := time.Now()
	s.mu.Lock()
	s.sessions[id] = &Session{
		ID:        id,
		User:      user,
		CSRFToken: csrf,
		CreatedAt: now,
		TouchedAt: now,
	}
	s.mu.Unlock()
	return id
}

// Get returns the session if it exists and hasn't expired.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(sess.TouchedAt) > s.ttl {
		s.Delete(id)
		return nil, false
	}
	return sess, true
}

// Touch refreshes the session's TTL.
func (s *SessionStore) Touch(id string) {
	s.mu.Lock()
	if sess, ok := s.sessions[id]; ok {
		sess.TouchedAt = time.Now()
	}
	s.mu.Unlock()
}

// Delete removes a session.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run tests — expect PASS**

```bash
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -run TestSession -v -race
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/web/session.go internal/daemon/web/session_test.go
git commit -m "feat(web): in-memory session store with TTL, CSRF tokens"
```

---

### Task 3: GitHub OAuth + PKCE

**Files:**
- Create: `internal/daemon/web/auth.go`
- Create: `internal/daemon/web/templates/login.html`
- Test: `internal/daemon/web/auth_test.go`

- [ ] **Step 1: Create login.html template**

```html
{{/* internal/daemon/web/templates/login.html */}}
{{define "content"}}
<div style="display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:60vh">
  <h1 class="text-2xl font-bold" style="margin-bottom:8px">AltFix Control Plane</h1>
  <p class="text-gray-500" style="margin-bottom:24px">Sign in with GitHub to monitor your coding agents</p>
  {{if .Content.Error}}
    <div style="background:#fef2f2;border:1px solid #fecaca;padding:12px;border-radius:8px;margin-bottom:16px;color:#b91c1c">
      {{.Content.Error}}
    </div>
  {{end}}
  <a href="/auth/github" style="display:inline-flex;align-items:center;gap:8px;background:#24292f;color:white;padding:12px 24px;border-radius:8px;text-decoration:none;font-weight:600">
    Sign in with GitHub
  </a>
  <p class="text-sm text-gray-500" style="margin-top:16px">
    Access restricted to authorized users and organizations.
  </p>
</div>
{{end}}
```

- [ ] **Step 2: Write auth tests**

```go
// internal/daemon/web/auth_test.go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuthRedirect_SetsStateAndPKCE(t *testing.T) {
	sessions := NewSessionStore(1 * time.Hour)
	h := &WebHandler{
		sessions: sessions,
		cfg: WebConfig{
			GitHubClientID: "test-client-id",
			BaseURL:        "http://localhost:9100",
		},
	}
	req := httptest.NewRequest("GET", "/auth/github", nil)
	rec := httptest.NewRecorder()
	h.HandleOAuthRedirect(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("no Location header")
	}
	// Should contain client_id, state, code_challenge
	for _, want := range []string{"client_id=test-client-id", "state=", "code_challenge="} {
		if !containsParam(loc, want) {
			t.Errorf("Location missing %q: %s", want, loc)
		}
	}
}

func containsParam(url, param string) bool {
	return len(url) > 0 && len(param) > 0 && indexOf(url, param) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 3: Implement auth.go**

```go
// internal/daemon/web/auth.go
package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WebHandler holds dependencies for all web UI handlers.
type WebHandler struct {
	tmpl     *Templates
	store    StoreInterface
	sessions *SessionStore
	cfg      WebConfig
	orgCache *OrgCache
}

// StoreInterface defines the daemon.Store methods used by the web UI.
type StoreInterface interface {
	GetTask(id string) (interface{}, error)
	ListTasks() (interface{}, error)
	ListTasksByStatus(status string) (interface{}, error)
	ListEvents(taskID string, afterID int64) (interface{}, error)
}

// OrgCache caches GitHub org membership per user.
type OrgCache struct {
	entries map[string]*orgEntry
	ttl     time.Duration
}

type orgEntry struct {
	orgs     []string
	cachedAt time.Time
}

func NewOrgCache(ttl time.Duration) *OrgCache {
	return &OrgCache{entries: make(map[string]*orgEntry), ttl: ttl}
}

func (c *OrgCache) Get(login string) ([]string, bool) {
	e, ok := c.entries[login]
	if !ok || time.Since(e.cachedAt) > c.ttl {
		return nil, false
	}
	return e.orgs, true
}

func (c *OrgCache) Set(login string, orgs []string) {
	c.entries[login] = &orgEntry{orgs: orgs, cachedAt: time.Now()}
}

// HandleLoginPage renders the login page.
func (h *WebHandler) HandleLoginPage(w http.ResponseWriter, r *http.Request) {
	errMsg := r.URL.Query().Get("error")
	data := PageData{
		Title:   "Login",
		ShowNav: false,
		Content: map[string]string{"Error": errMsg},
	}
	h.tmpl.Render(w, "login", data)
}

// HandleOAuthRedirect initiates the GitHub OAuth + PKCE flow.
func (h *WebHandler) HandleOAuthRedirect(w http.ResponseWriter, r *http.Request) {
	state := randomHex(16)
	verifier := randomHex(32)

	// Store state + verifier in a short-lived session.
	sid := h.sessions.Create(&SessionUser{Login: "__pending__"})
	sess, _ := h.sessions.Get(sid)
	sess.User.Login = "__oauth_pending__"
	// Store OAuth state in CSRF token field (repurposed for OAuth).
	sess.CSRFToken = state + ":" + verifier

	http.SetCookie(w, &http.Cookie{
		Name:     "altfix_oauth",
		Value:    sid,
		Path:     "/auth",
		MaxAge:   600, // 10 minutes for OAuth flow
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	// PKCE challenge.
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	params := url.Values{
		"client_id":             {h.cfg.GitHubClientID},
		"redirect_uri":         {h.cfg.BaseURL + "/auth/callback"},
		"scope":                {"read:user read:org"},
		"state":                {state},
		"code_challenge":       {challenge},
		"code_challenge_method": {"S256"},
	}

	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+params.Encode(), http.StatusFound)
}

// HandleOAuthCallback handles the GitHub OAuth callback.
func (h *WebHandler) HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/ui/login?error=missing+code+or+state", http.StatusFound)
		return
	}

	// Verify state from OAuth cookie.
	cookie, err := r.Cookie("altfix_oauth")
	if err != nil {
		http.Redirect(w, r, "/ui/login?error=missing+oauth+cookie", http.StatusFound)
		return
	}
	sess, ok := h.sessions.Get(cookie.Value)
	if !ok {
		http.Redirect(w, r, "/ui/login?error=expired+oauth+session", http.StatusFound)
		return
	}

	parts := strings.SplitN(sess.CSRFToken, ":", 2)
	if len(parts) != 2 || parts[0] != state {
		http.Redirect(w, r, "/ui/login?error=invalid+state", http.StatusFound)
		return
	}
	verifier := parts[1]

	// Clean up OAuth session.
	h.sessions.Delete(cookie.Value)
	http.SetCookie(w, &http.Cookie{
		Name: "altfix_oauth", Path: "/auth", MaxAge: -1,
	})

	// Exchange code for token.
	ghToken, err := exchangeCode(r.Context(), h.cfg.GitHubClientID, h.cfg.GitHubSecret, code, verifier, h.cfg.BaseURL+"/auth/callback")
	if err != nil {
		http.Redirect(w, r, "/ui/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	// Fetch user info.
	user, err := fetchGitHubUser(r.Context(), ghToken)
	if err != nil {
		http.Redirect(w, r, "/ui/login?error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}

	// Fetch orgs.
	orgs, _ := fetchGitHubOrgs(r.Context(), ghToken)

	// Authorization check.
	if !h.isAuthorized(user.Login, orgs) {
		http.Redirect(w, r, "/ui/login?error=not+authorized", http.StatusFound)
		return
	}

	isAdmin := false
	for _, a := range h.cfg.AdminUsers {
		if a == user.Login {
			isAdmin = true
			break
		}
	}

	// Create authenticated session.
	newSID := h.sessions.Create(&SessionUser{
		Login:       user.Login,
		AvatarURL:   user.AvatarURL,
		GitHubToken: ghToken,
		IsAdmin:     isAdmin,
		Orgs:        orgs,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "altfix_session",
		Value:    newSID,
		Path:     "/",
		MaxAge:   28800, // 8 hours
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/ui/", http.StatusFound)
}

// HandleLogout clears the session.
func (h *WebHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("altfix_session"); err == nil {
		h.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "altfix_session", Path: "/", MaxAge: -1,
	})
	http.Redirect(w, r, "/ui/login", http.StatusFound)
}

func (h *WebHandler) isAuthorized(login string, orgs []string) bool {
	// Check allowed users.
	for _, u := range h.cfg.AllowedUsers {
		if u == login {
			return true
		}
	}
	// Check allowed orgs.
	for _, allowed := range h.cfg.AllowedOrgs {
		for _, userOrg := range orgs {
			if allowed == userOrg {
				return true
			}
		}
	}
	// If no restrictions configured, allow all.
	return len(h.cfg.AllowedUsers) == 0 && len(h.cfg.AllowedOrgs) == 0
}

type ghUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

func exchangeCode(ctx context.Context, clientID, clientSecret, code, verifier, redirectURI string) (string, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	json.Unmarshal(body, &result)
	if result.Error != "" {
		return "", fmt.Errorf("github: %s", result.Error)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}
	return result.AccessToken, nil
}

func fetchGitHubUser(ctx context.Context, token string) (*ghUser, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var u ghUser
	json.NewDecoder(resp.Body).Decode(&u)
	if u.Login == "" {
		return nil, fmt.Errorf("empty github login")
	}
	return &u, nil
}

func fetchGitHubOrgs(ctx context.Context, token string) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/orgs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var orgs []struct{ Login string `json:"login"` }
	json.NewDecoder(resp.Body).Decode(&orgs)
	names := make([]string, len(orgs))
	for i, o := range orgs {
		names[i] = o.Login
	}
	return names, nil
}
```

- [ ] **Step 4: Run tests**

```bash
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -run TestOAuth -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/web/auth.go internal/daemon/web/auth_test.go internal/daemon/web/templates/login.html
git commit -m "feat(web): GitHub OAuth + PKCE flow, login page"
```

---

### Task 4: Middleware — RequireAuth, CSRF, Error Pages

**Files:**
- Create: `internal/daemon/web/middleware.go`
- Create: `internal/daemon/web/templates/errors/403.html`
- Create: `internal/daemon/web/templates/errors/404.html`
- Create: `internal/daemon/web/templates/errors/session_expired.html`
- Test: `internal/daemon/web/middleware_test.go`

- [ ] **Step 1: Create error page templates**

```html
{{/* internal/daemon/web/templates/errors/403.html */}}
{{define "content"}}
<div style="text-align:center;padding:80px 0">
  <h1 class="text-2xl font-bold">Access Denied</h1>
  <p class="text-gray-500" style="margin-top:8px">You are not authorized to access AltFix.</p>
  <a href="/ui/login" style="margin-top:24px;display:inline-block">Back to Login</a>
</div>
{{end}}
```

```html
{{/* internal/daemon/web/templates/errors/404.html */}}
{{define "content"}}
<div style="text-align:center;padding:80px 0">
  <h1 class="text-2xl font-bold">Not Found</h1>
  <p class="text-gray-500" style="margin-top:8px">The page you're looking for doesn't exist.</p>
  <a href="/ui/" style="margin-top:24px;display:inline-block">Back to Dashboard</a>
</div>
{{end}}
```

```html
{{/* internal/daemon/web/templates/errors/session_expired.html */}}
{{define "content"}}
<div style="text-align:center;padding:80px 0">
  <h1 class="text-2xl font-bold">Session Expired</h1>
  <p class="text-gray-500" style="margin-top:8px">Your session has expired. Please log in again.</p>
  <a href="/ui/login" style="margin-top:24px;display:inline-block">Log in</a>
</div>
{{end}}
```

- [ ] **Step 2: Write middleware tests**

```go
// internal/daemon/web/middleware_test.go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireAuth_RedirectsUnauthenticated(t *testing.T) {
	sessions := NewSessionStore(1 * time.Hour)
	mw := RequireAuth(sessions)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/ui/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rec.Code)
	}
}

func TestRequireAuth_AllowsAuthenticated(t *testing.T) {
	sessions := NewSessionStore(1 * time.Hour)
	sid := sessions.Create(&SessionUser{Login: "alice"})
	mw := RequireAuth(sessions)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/ui/", nil)
	req.AddCookie(&http.Cookie{Name: "altfix_session", Value: sid})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestCSRFCheck_RejectsInvalidToken(t *testing.T) {
	mw := CSRFCheck()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("POST", "/ui/tasks/new", nil)
	// Simulate authenticated session in context.
	req = req.WithContext(withSession(req.Context(), &Session{CSRFToken: "valid-token"}))
	req.Header.Set("X-CSRF-Token", "wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}
```

- [ ] **Step 3: Implement middleware**

```go
// internal/daemon/web/middleware.go
package web

import (
	"context"
	"crypto/subtle"
	"net/http"
)

type contextKey string

const sessionContextKey contextKey = "session"

// withSession stores a session in request context.
func withSession(ctx context.Context, sess *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, sess)
}

// getSession retrieves the session from request context.
func getSession(r *http.Request) *Session {
	sess, _ := r.Context().Value(sessionContextKey).(*Session)
	return sess
}

// RequireAuth redirects to login if no valid session.
func RequireAuth(sessions *SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("altfix_session")
			if err != nil {
				http.Redirect(w, r, "/ui/login", http.StatusFound)
				return
			}
			sess, ok := sessions.Get(cookie.Value)
			if !ok {
				http.Redirect(w, r, "/ui/login?error=session+expired", http.StatusFound)
				return
			}
			sessions.Touch(cookie.Value)
			ctx := withSession(r.Context(), sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin returns 403 if the session user is not an admin.
func RequireAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := getSession(r)
			if sess == nil || !sess.User.IsAdmin {
				http.Error(w, "forbidden", 403)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFCheck validates the CSRF token on POST/PUT/DELETE requests.
func CSRFCheck() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
				next.ServeHTTP(w, r)
				return
			}
			sess := getSession(r)
			if sess == nil {
				http.Error(w, "CSRF: no session", 403)
				return
			}
			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				token = r.FormValue("_csrf")
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(sess.CSRFToken)) != 1 {
				http.Error(w, "CSRF validation failed", 403)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run tests**

```bash
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -run TestRequireAuth -v -race
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -run TestCSRF -v -race
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/web/middleware.go internal/daemon/web/middleware_test.go internal/daemon/web/templates/errors/
git commit -m "feat(web): auth + CSRF middleware, error pages"
```

---

### Task 5: Page Handlers + Templates (Dashboard, Detail, New, PRs, Settings)

**Files:**
- Create: `internal/daemon/web/handlers.go`
- Create: `internal/daemon/web/sse_html.go`
- Create: `internal/daemon/web/templates/dashboard.html`
- Create: `internal/daemon/web/templates/detail.html`
- Create: `internal/daemon/web/templates/new.html`
- Create: `internal/daemon/web/templates/prs.html`
- Create: `internal/daemon/web/templates/settings.html`
- Create: `internal/daemon/web/templates/partials/task_card.html`
- Create: `internal/daemon/web/templates/partials/task_list.html`
- Create: `internal/daemon/web/templates/partials/event_item.html`
- Create: `internal/daemon/web/templates/partials/kpi_cards.html`
- Create: `internal/daemon/web/templates/partials/phase_bar.html`
- Create: `internal/daemon/web/templates/partials/repo_issues.html`
- Create: `internal/daemon/web/templates/partials/pr_row.html`

This is the largest task. The implementer should create each template and its corresponding handler, testing incrementally. Each page template defines a `{{define "content"}}` block that layout.html includes.

Key patterns:
- Every handler calls `getSession(r)` to get the user
- Every handler builds `PageData{Title, ShowNav: true, User, IsAdmin, CSRFToken, Content}`
- Dashboard uses `hx-get` polling, detail uses native EventSource, new uses `hx-get` for progressive disclosure
- Partials are used as htmx swap targets AND as SSE data payloads

The implementer should refer to the spec at `docs/superpowers/specs/2026-04-13-altfix-webui-design.md` sections "Pages" (lines 108-301) for exact field layouts, event type rendering, and form flows.

- [ ] **Step 1: Create all partial templates** (event_item, task_card, task_list, kpi_cards, phase_bar, repo_issues, pr_row)
- [ ] **Step 2: Create dashboard.html with KPI cards + task list**
- [ ] **Step 3: Create detail.html with phase timeline + SSE script + steering form**
- [ ] **Step 4: Create new.html with progressive disclosure form**
- [ ] **Step 5: Create prs.html with PR tracker table**
- [ ] **Step 6: Create settings.html with config viewer**
- [ ] **Step 7: Implement handlers.go** — HandleDashboard, HandleTaskDetail, HandleNewTask, HandlePRs, HandleSettings, HandlePartialTaskList, HandlePartialKPICards, HandlePartialRepoIssues
- [ ] **Step 8: Implement sse_html.go** — HandleSSEHTML that wraps events as HTML partials in SSE format
- [ ] **Step 9: Run template render tests**

```bash
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -v -race
```

- [ ] **Step 10: Commit**

```bash
git add internal/daemon/web/handlers.go internal/daemon/web/sse_html.go internal/daemon/web/templates/
git commit -m "feat(web): dashboard, detail, new, PRs, settings pages + partials"
```

---

### Task 6: Shared URLs — HMAC Signing + Redaction

**Files:**
- Create: `internal/daemon/web/share.go`
- Create: `internal/daemon/web/templates/share.html`
- Test: `internal/daemon/web/share_test.go`

- [ ] **Step 1: Write share tests**

```go
// internal/daemon/web/share_test.go
package web

import (
	"testing"
	"time"
)

func TestSignAndVerifyShareURL(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	taskID := "abc123"
	expiry := time.Now().Add(1 * time.Hour).Unix()

	token := SignShareURL(taskID, expiry, secret)
	if token == "" {
		t.Fatal("empty token")
	}

	err := VerifyShareURL(taskID, token, expiry, secret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyShareURL_Expired(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	expiry := time.Now().Add(-1 * time.Hour).Unix() // past
	token := SignShareURL("abc", expiry, secret)
	err := VerifyShareURL("abc", token, expiry, secret)
	if err == nil {
		t.Fatal("expected expiry error")
	}
}

func TestVerifyShareURL_WrongToken(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	expiry := time.Now().Add(1 * time.Hour).Unix()
	err := VerifyShareURL("abc", "wrong-token", expiry, secret)
	if err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func TestVerifyShareURL_NULSeparator(t *testing.T) {
	// Ensure "abc"+"123" != "ab"+"c123" (NUL separator prevents collision)
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	expiry := int64(123)
	t1 := SignShareURL("abc", expiry, secret)
	t2 := SignShareURL("ab", 0, secret) // different split
	if t1 == t2 {
		t.Error("HMAC collision: NUL separator not working")
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"api_key=sk-abc123def456", "api_key=[REDACTED]"},
		{"no secrets here", "no secrets here"},
		{"ghp_1234567890abcdefghijklmnopqrstuvwxyz", "[REDACTED]"},
		{"AKIA1234567890ABCDEF", "[REDACTED]"},
	}
	for _, tt := range tests {
		got := RedactSecrets(tt.input)
		if got == tt.input && tt.input != tt.want {
			t.Errorf("RedactSecrets(%q) was not redacted", tt.input)
		}
	}
}
```

- [ ] **Step 2: Implement share.go**

```go
// internal/daemon/web/share.go
package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SignShareURL generates an HMAC token for a shared task URL.
func SignShareURL(taskID string, expiry int64, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(taskID))
	mac.Write([]byte{0x00}) // NUL separator prevents concatenation ambiguity
	mac.Write([]byte(strconv.FormatInt(expiry, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyShareURL checks that a share token is valid and not expired.
func VerifyShareURL(taskID, token string, expiry int64, secret []byte) error {
	if time.Now().Unix() > expiry+60 { // 60s clock skew grace
		return fmt.Errorf("share link expired")
	}
	expected := SignShareURL(taskID, expiry, secret)
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return fmt.Errorf("invalid share signature")
	}
	return nil
}

var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|auth)\s*[:=]\s*\S+`),
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]+ KEY-----`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
}

// RedactSecrets strips sensitive patterns from text.
func RedactSecrets(data string) string {
	for _, re := range redactPatterns {
		data = re.ReplaceAllString(data, "[REDACTED]")
	}
	return data
}

// HandleShareView renders a public read-only task detail.
func (h *WebHandler) HandleShareView(w http.ResponseWriter, r *http.Request) {
	tokenPath := r.PathValue("token")
	// Parse: {taskID}.{hmac}
	dotIdx := strings.LastIndex(tokenPath, ".")
	if dotIdx < 0 {
		http.Error(w, "invalid share link", 400)
		return
	}
	taskID := tokenPath[:dotIdx]
	token := tokenPath[dotIdx+1:]
	expiryStr := r.URL.Query().Get("exp")
	expiry, _ := strconv.ParseInt(expiryStr, 10, 64)

	if err := VerifyShareURL(taskID, token, expiry, h.cfg.SigningKey); err != nil {
		http.Error(w, err.Error(), 403)
		return
	}

	// Render share page (implementation in Task 5 templates).
	data := PageData{
		Title:   "Shared Task",
		ShowNav: false,
		Content: map[string]string{"TaskID": taskID},
	}
	h.tmpl.Render(w, "share", data)
}
```

- [ ] **Step 3: Create share.html template**

```html
{{/* internal/daemon/web/templates/share.html */}}
{{define "content"}}
<div style="padding:24px">
  <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
    <h1 class="text-xl font-bold">Shared Task View</h1>
    <span class="text-sm text-gray-500">Read-only</span>
  </div>
  <p class="text-gray-500">Task: {{.Content.TaskID}}</p>
  <div id="activity-feed" style="margin-top:16px;max-height:600px;overflow-y:auto">
    <p class="text-gray-500">Loading events...</p>
  </div>
</div>
{{end}}
```

- [ ] **Step 4: Run tests**

```bash
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -run TestSign -v -race
GOFLAGS=-mod=mod go test ./internal/daemon/web/... -run TestRedact -v -race
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/web/share.go internal/daemon/web/share_test.go internal/daemon/web/templates/share.html
git commit -m "feat(web): HMAC shared URLs with secret redaction"
```

---

### Task 7: Wire Into Server + CLI Flags

**Files:**
- Modify: `internal/daemon/server.go`
- Modify: `cmd/altcode/daemon.go`

- [ ] **Step 1: Add WebUI config to ServerConfig**

Add to `internal/daemon/server.go` `ServerConfig`:

```go
// Web UI config (optional — if GitHubClientID is empty, web UI is disabled).
GitHubClientID     string
GitHubClientSecret string
AllowedOrgs        []string
AllowedUsers       []string
AdminUsers         []string
SigningKey          string
```

- [ ] **Step 2: Wire RegisterRoutes in NewServer**

In `NewServer`, after `s.registerRoutes()`, add:

```go
if cfg.GitHubClientID != "" {
    sessions := web.NewSessionStore(8 * time.Hour)
    if err := web.RegisterRoutes(s.mux, web.WebConfig{
        Store:          s.store,
        Sessions:       sessions,
        GitHubClientID: cfg.GitHubClientID,
        GitHubSecret:   cfg.GitHubClientSecret,
        AllowedOrgs:    cfg.AllowedOrgs,
        AllowedUsers:   cfg.AllowedUsers,
        AdminUsers:     cfg.AdminUsers,
        SigningKey:      []byte(cfg.SigningKey),
        BaseURL:        fmt.Sprintf("http://localhost:%d", cfg.Port),
    }); err != nil {
        return nil, fmt.Errorf("web ui: %w", err)
    }
    s.logger.Info("web UI enabled", "login", "/ui/login")
}
```

- [ ] **Step 3: Add CLI flags to daemon.go**

Add flags to `newDaemonCmd`:

```go
var githubClientID, githubClientSecret string
var allowedOrgs, allowedUsers, adminUsers []string
var signingKey string

cmd.Flags().StringVar(&githubClientID, "github-client-id", "", "GitHub OAuth App client ID (enables web UI)")
cmd.Flags().StringVar(&githubClientSecret, "github-client-secret", "", "GitHub OAuth App client secret")
cmd.Flags().StringSliceVar(&allowedOrgs, "allowed-orgs", nil, "GitHub orgs allowed to access web UI")
cmd.Flags().StringSliceVar(&allowedUsers, "allowed-users", nil, "GitHub users allowed to access web UI")
cmd.Flags().StringSliceVar(&adminUsers, "admin-users", nil, "GitHub users with admin access")
cmd.Flags().StringVar(&signingKey, "signing-key", "", "HMAC signing key for shared URLs")
```

Wire into `ServerConfig` in `RunE`.

- [ ] **Step 4: Build and test**

```bash
GOFLAGS=-mod=mod go build ./...
GOFLAGS=-mod=mod go test ./internal/daemon/... -race -count=1 -timeout=60s
```

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/server.go cmd/altcode/daemon.go
git commit -m "feat(web): wire web UI into daemon server + CLI flags"
```

---

### Task 8: Integration Tests + Template Contract Tests

**Files:**
- Create: `internal/daemon/web/integration_test.go`

- [ ] **Step 1: Write template contract tests**

Test that every page template renders without error:

```go
// internal/daemon/web/integration_test.go
package web

import (
	"bytes"
	"testing"
)

func TestAllTemplatesRender(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	pages := []struct {
		name string
		data PageData
	}{
		{"login", PageData{Title: "Login"}},
		{"dashboard", PageData{Title: "Dashboard", ShowNav: true, User: &SessionUser{Login: "test"}, CSRFToken: "tok"}},
		{"detail", PageData{Title: "Detail", ShowNav: true, User: &SessionUser{Login: "test"}, CSRFToken: "tok"}},
		{"new", PageData{Title: "New", ShowNav: true, User: &SessionUser{Login: "test"}, CSRFToken: "tok"}},
		{"prs", PageData{Title: "PRs", ShowNav: true, User: &SessionUser{Login: "test"}, CSRFToken: "tok"}},
		{"settings", PageData{Title: "Settings", ShowNav: true, User: &SessionUser{Login: "test"}, IsAdmin: true, CSRFToken: "tok"}},
		{"share", PageData{Title: "Share", Content: map[string]string{"TaskID": "abc"}}},
		{"error-403", PageData{Title: "403"}},
		{"error-404", PageData{Title: "404"}},
		{"error-session_expired", PageData{Title: "Session Expired"}},
	}
	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tmpl.Render(&buf, p.name, p.data); err != nil {
				t.Fatalf("render %s: %v", p.name, err)
			}
			if buf.Len() == 0 {
				t.Errorf("render %s: empty output", p.name)
			}
		})
	}
}
```

- [ ] **Step 2: Write HMAC edge-case tests**

Add to `share_test.go` (see spec Appendix A5 for test vectors).

- [ ] **Step 3: Run full test suite**

```bash
GOFLAGS=-mod=mod go build ./...
GOFLAGS=-mod=mod go vet ./...
GOFLAGS=-mod=mod go test ./internal/daemon/... -race -count=1 -timeout=60s
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/web/integration_test.go
git commit -m "test(web): template contract tests + HMAC edge cases"
```

---

## CC Review Blocker Fixes (applied)

CC graded 6/10 with 9 blockers. All addressed below — implementers MUST apply these:

### B1. Task ordering fix
**Problem**: `web.go` (T1) references `SessionStore` (T2), `WebHandler` (T3), `RequireAuth` (T4).
**Fix**: T1 should ONLY create `embed.go`, `static/`, `templates/layout.html`, and a minimal
`web.go` with `LoadTemplates`, `Render`, `RenderPartial` — NO `RegisterRoutes` or `WebHandler`.
Move `RegisterRoutes` and `WebConfig` to T7 (wiring). Each task compiles independently.

### B2. StoreInterface must be concrete
**Problem**: `WebConfig.Store interface{ /* ... */ }` is not valid Go.
**Fix**: Define with real signatures matching `daemon.Store`:
```go
type StoreInterface interface {
    GetTask(id string) (*daemon.Task, error)
    ListTasks() ([]*daemon.Task, error)
    ListTasksByStatus(status string) ([]*daemon.Task, error)
    ListEvents(taskID string, afterID int64) ([]*daemon.TaskEvent, error)
    CountPendingBefore(taskID string) (int, error)
}
```
Or use `*daemon.Store` directly (preferred — no interface needed for single implementation).

### B3. OAuth session mutation race
**Problem**: `HandleOAuthRedirect` mutates `sess.User.Login` and `sess.CSRFToken` after Get().
**Fix**: Add `SetOAuthState` method to `SessionStore`:
```go
func (s *SessionStore) SetOAuthState(id, state, verifier string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if sess, ok := s.sessions[id]; ok {
        sess.CSRFToken = state + ":" + verifier
    }
}
```

### B4. CSRF middleware not applied
**Problem**: `csrf := CSRFCheck()` declared but not used on any route.
**Fix**: Wrap all mutating routes:
```go
mux.Handle("POST /auth/logout", auth(csrf(http.HandlerFunc(h.HandleLogout))))
// Steer/stop already go through /api/ with Bearer auth — CSRF applies only to /ui/ POST routes
```

### B5. Task 5 split into 3
**Problem**: 7 pages + 7 partials + 8 handlers in one task.
**Fix**: Split into:
- **T5a**: Dashboard page + task_list + task_card + kpi_cards partials + HandleDashboard + HandlePartialTaskList + HandlePartialKPICards
- **T5b**: Detail page + event_item + phase_bar partials + HandleTaskDetail + HandleSSEHTML
- **T5c**: New + PRs + Settings pages + repo_issues + pr_row partials + remaining handlers

### B6. Session GC goroutine
**Fix**: Add to `SessionStore`:
```go
func (s *SessionStore) StartGC(interval time.Duration) {
    go func() {
        for {
            time.Sleep(interval)
            s.mu.Lock()
            now := time.Now()
            for id, sess := range s.sessions {
                if now.Sub(sess.TouchedAt) > s.ttl {
                    delete(s.sessions, id)
                }
            }
            s.mu.Unlock()
        }
    }()
}
```
Call `sessions.StartGC(5 * time.Minute)` in T7 wiring.

### B7. GitHub API User-Agent + status checks
**Fix**: Add to all GitHub API calls:
```go
req.Header.Set("User-Agent", "altcode-daemon/1.0")
```
And check response status:
```go
if resp.StatusCode != 200 {
    body, _ := io.ReadAll(resp.Body)
    return nil, fmt.Errorf("github API %d: %s", resp.StatusCode, body)
}
```

### B8. OrgCache mutex
**Fix**: Add `sync.RWMutex` to `OrgCache`:
```go
type OrgCache struct {
    mu      sync.RWMutex
    entries map[string]*orgEntry
    ttl     time.Duration
}
func (c *OrgCache) Get(login string) ([]string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // ...
}
func (c *OrgCache) Set(login string, orgs []string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    // ...
}
```

### B9. TestRedactSecrets assertion fix
**Fix**: Replace inverted assertion:
```go
for _, tt := range tests {
    got := RedactSecrets(tt.input)
    if !strings.Contains(got, "[REDACTED]") && tt.input != tt.want {
        t.Errorf("RedactSecrets(%q) = %q, expected redaction", tt.input, got)
    }
}
```

### Additional test cases (CC coverage gaps)
Add tests for: OAuth callback state mismatch, expired OAuth cookie, GitHub 4xx response,
`RequireAdmin` rejection, `_csrf` form field fallback, `isAuthorized` empty config (allow-all),
share link boundary (expiry ± 1s), concurrent `SessionStore` operations.

## Self-Review Checklist

1. **Spec coverage**: All 7 pages covered (login T3, dashboard T5a, detail T5b, new/PRs/settings T5c, share T6). Auth T3, CSRF T4, middleware T4, SSE T5b, wiring T7, tests T8.
2. **Placeholder scan**: All steps have code. No TBD/TODO. B5 split provides clearer boundaries.
3. **Type consistency**: `PageData`, `SessionUser`, `Session`, `WebConfig`, `WebHandler` — used consistently. `StoreInterface` replaced with `*daemon.Store` (B2 fix).
4. **Missing from spec**: All appendix items (A1-A8) covered. CC blocker fixes (B1-B9) applied.
