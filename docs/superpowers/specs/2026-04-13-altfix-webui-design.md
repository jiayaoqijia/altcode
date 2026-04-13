# AltFix Web UI — Control Plane Design (v1)

> **For agentic workers:** This is a design spec for the AltFix control plane web UI,
> embedded in the `altcode daemon` binary. The primary interaction surface is GitHub
> (issues in, PRs out). This UI is the ops dashboard, not an IDE replacement.

## Problem

AltFix daemon runs autonomously — GitHub webhooks trigger issue-fixing pipelines
that spawn codex/claude agents, create PRs, and auto-fix CI failures. But operators
have no visibility into what AltFix is doing without SSH access and `curl` commands.
Teams can't share task status. There's no cost monitoring or success tracking.

## Non-Goals

- **Not an IDE** — no Monaco editor, no file browser, no terminal emulator
- **Not a chat UI** — no conversation interface, no prompt input for general coding
- **Not a replacement for GitHub** — PRs, reviews, and merges stay in GitHub
- **Not multi-tenant SaaS** — single daemon instance, team access via GitHub OAuth

## Architecture

Single Go binary serves both JSON API and HTML UI. Zero separate frontend deploy.

```
altcode daemon --port 9100
  │
  ├── /api/tasks/*           → JSON REST API (existing, 230+ tests)
  ├── /api/tasks/:id/sse     → SSE streaming (existing)
  ├── /api/tasks/:id/steer   → Steering (existing)
  ├── /api/webhooks/github   → Webhook receiver (existing)
  │
  ├── /auth/github           → OAuth redirect (NEW)
  ├── /auth/callback         → OAuth callback (NEW)
  ├── /auth/logout           → Clear session (NEW)
  │
  ├── /ui/                   → Dashboard (NEW, embed.FS)
  ├── /ui/tasks/:id          → Task detail (NEW)
  ├── /ui/tasks/new          → Create task (NEW)
  ├── /ui/prs                → PR tracker (NEW)
  ├── /ui/settings           → Config + webhooks (NEW)
  │
  └── /share/:token          → Public read-only view (NEW)
```

### Tech Stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Templates | Go `html/template` | Zero build step, type-safe |
| Interactivity | htmx 2.x (vendored) | `hx-sse` connects to existing SSE, forms are POST |
| Styling | Tailwind CSS (vendored, purged) | ~20KB, dark mode, no CDN (air-gap safe) |
| Bundling | Go `embed.FS` | Ships in binary, zero npm |
| Auth | GitHub OAuth + PKCE | Team identity, org-gated |
| Sessions | `HttpOnly`/`Secure`/`SameSite=Lax` cookie | No localStorage tokens |
| CSRF | Double-submit cookie pattern | All POST forms include token via `hx-headers` |
| Shared URLs | HMAC-SHA256 signed tokens | Opaque, expiring, revocable |

### Why Not React

| Concern | htmx | React |
|---------|------|-------|
| Build pipeline | None | npm + Vite + node_modules |
| Binary size | +50KB (htmx + CSS) | +2MB (React + deps) |
| Deploy | Same binary | Separate static deploy or embed dist/ |
| SSE integration | `hx-sse` native | Custom EventSource + state management |
| Steering | `<form hx-post>` | useState + fetch + loading state |
| Maintenance | Go templates only | Two languages, two build systems |

CC and Codex both independently recommended htmx over React for this use case.

## Auth

### GitHub OAuth with PKCE

```
Browser → /auth/github → GitHub authorize URL (with state + code_verifier)
GitHub → /auth/callback?code=X&state=Y → daemon exchanges code for token
Daemon → fetches user info, checks allowed_users/allowed_orgs
Daemon → sets session cookie, redirects to /ui/
```

### Authorization

OAuth authenticates identity. Authorization is separate:

```jsonc
// daemon config or --allowed-orgs flag
{
  "web_ui": {
    "allowed_orgs": ["altcode-ai"],       // GitHub org membership required
    "allowed_users": ["jiayaoqijia"],     // OR specific usernames
    "admin_users": ["jiayaoqijia"],       // can edit settings, manage webhooks
    "viewer_default": true                 // non-admin = read-only + steer
  }
}
```

Users not in `allowed_orgs` or `allowed_users` see a 403 page.

### Session

- Cookie: `altfix_session`, `HttpOnly`, `Secure`, `SameSite=Lax`
- TTL: 8 hours, refreshed on activity
- Storage: in-memory session map (daemon restart = re-login, acceptable for v1)
- Logout: `POST /auth/logout` clears cookie + server session

## Pages

### 1. Dashboard (`/ui/`)

The landing page after login. Shows operational overview + active task queue.

**KPI row** (4 cards, auto-refresh every 30s via `hx-trigger="every 30s"`):

| Card | Metric | Source |
|------|--------|--------|
| Active | Running tasks / max concurrent | `store.ListTasksByStatus("implementing", "reviewing", ...)` |
| Queue | Pending tasks waiting for a slot | `store.ListTasksByStatus("pending")` |
| Cost (24h) | Total API cost in last 24 hours | `SUM(api_cost_usd) WHERE created_at > now()-24h` |
| Success | Completed / (completed + failed) % | `store.ListTasks()` aggregation |

**Task list** (below KPIs):

```html
<!-- Auto-refreshes task list every 5s -->
<div hx-get="/ui/partials/task-list?status=active"
     hx-trigger="every 5s"
     hx-swap="innerHTML">
```

Each task card shows:
- Status dot (color-coded: pending=amber, running=green, failed=red, merged=blue)
- Issue reference: `owner/repo#42` (linked to GitHub)
- Task summary (first 80 chars)
- Pipeline phase: `plan ✓ → implement ⟳ → review · → test ·`
- Cost so far
- Time elapsed
- Trigger type badge (webhook / manual / comment)

**Filters**: status (all / active / completed / failed), repo, date range.

### 2. Task Detail (`/ui/tasks/:id`)

Live view of a single task's execution. The core monitoring page.

**Header**:
- Back to dashboard link
- Task title (issue number + summary)
- Status badge (color-coded)
- Cost, duration
- PR link (if created, links to GitHub)
- Cancel button (with confirmation)

**Phase timeline** (horizontal bar):
```
[plan ✓] → [implement ⟳] → [review ·] → [test ·] → [PR ·]
```
Each phase is color-coded: ✓ green, ⟳ amber animated, · gray. Inspired by
OpenHands Eval Monitor's StatusTimeline component.

**Activity feed** (live SSE stream):

Uses native `EventSource` API (not htmx-sse extension — Codex flagged it as fragile).
A ~30-line `<script>` handles connection, reconnect, and DOM append:

```html
<script>
  const feed = document.getElementById('activity-feed');
  const url = '/api/tasks/{{.Task.ID}}/sse';
  let es;
  function connect() {
    es = new EventSource(url);
    es.onmessage = function(e) {
      // Server sends pre-rendered HTML partials as SSE data
      feed.insertAdjacentHTML('beforeend', e.data);
      feed.lastElementChild.scrollIntoView({behavior: 'smooth'});
      document.getElementById('reconnect-banner')?.remove();
    };
    es.onerror = function() {
      // Show reconnect banner; EventSource auto-reconnects
      if (!document.getElementById('reconnect-banner')) {
        feed.insertAdjacentHTML('afterbegin',
          '<div id="reconnect-banner" class="bg-amber-100 p-2">Reconnecting...</div>');
      }
    };
  }
  connect();
</script>
```

**Last-Event-ID replay** (CC blocker): the existing SSE endpoint already supports
`Last-Event-ID` header. `EventSource` sends it automatically on reconnect. Events
are never lost — the server replays from the last received ID.

Events are rendered as server-side HTML partials, type-specific:

| Event type | Rendering |
|------------|-----------|
| `phase_started` | Bold header: "Implement phase started" with icon |
| `phase_completed` | Green checkmark header |
| `agent_output` | Monospace block with role badge (lead/implementer/reviewer) |
| `tool_call` | Collapsible: tool name + duration + truncated input |
| `error` | Red banner with message |
| `user_steer` | Blue bubble: user's steering message |
| `pr_created` | Link to GitHub PR |
| `ci_status` | CI pass/fail badge |

**DOM management**: Keep last 200 events visible. Events beyond 200 collapse into
"Show N earlier events..." link that loads via `hx-get` on click. Prevents unbounded
DOM growth (Codex review finding).

**Steering input** (when task is active):
```html
<form hx-post="/api/tasks/{id}/steer"
      hx-headers='{"X-CSRF-Token": "{{.CSRFToken}}"}'
      hx-on::after-request="this.reset()">
  <input name="message" placeholder="Steer the agent..." required>
  <button type="submit">Send</button>
</form>
```
Disabled when task is terminal. CSRF token included.

**Reconnect handling**: If SSE disconnects (heartbeat timeout), show amber
"Reconnecting..." banner. `EventSource` auto-reconnects natively. Banner clears
on first received event.

### 3. Create Task (`/ui/tasks/new`)

Progressive disclosure form for manual task submission (complement to webhooks):

**Step 1**: Select repository (dropdown populated from user's GitHub repos)
```html
<select hx-get="/ui/partials/repo-issues?repo={{value}}"
        hx-target="#issue-section"
        hx-trigger="change">
```

**Step 2** (appears after repo selection): Choose input mode:
- **Pick issue**: dropdown of open issues from selected repo
- **Describe task**: freeform textarea + optional branch name

**Step 3**: Optional overrides:
- Model override (altllm-basic / altllm-standard / default)
- Max cost ($)
- Max turns

**Submit**: `POST /api/tasks` → redirect to `/ui/tasks/:id`

### 4. PR Tracker (`/ui/prs`)

Tracks PRs created by AltFix across all repos. GitHub is the source of truth;
this page aggregates for quick scanning.

**Columns**: repo, PR number (linked), title, status (draft/open/merged/closed),
CI status, review status, created, cost.

**Filters**: repo, status, date range.

**Data source**: tasks with `pr_url` set, enriched with stored CI/review status.
Auto-refreshes every 30s.

### 5. Settings (`/ui/settings`)

Admin-only page (non-admins see read-only view).

**Sections**:
- **Connected GitHub**: username, avatar, connected repos list
- **Webhook config**: repos with webhooks installed, secret rotation
- **Budget**: daily cost cap, per-task cost cap, max concurrent tasks
- **Model routing**: default model per role (lead/implementer/reviewer/tester)
- **Allowed users/orgs**: who can access this dashboard

### 6. Shared Task View (`/share/:token`)

Public read-only view of a task. No auth required. For sharing task progress
with stakeholders who don't have GitHub access.

**URL format**: `/share/{taskID}.{hmac}?exp={unix_timestamp}`

**Token generation**:
```go
mac := hmac.New(sha256.New, []byte(daemonSecret))
mac.Write([]byte(taskID + strconv.FormatInt(expiry, 10)))
token := hex.EncodeToString(mac.Sum(nil))
```

**Properties**:
- Default expiry: 24 hours
- Revocable via `DELETE /api/shares/:id` (stored in SQLite `shares` table)
- **Data redaction**: env vars, API keys, file contents with secrets are stripped
  from event data before rendering
- Read-only: no steering, no stop, no auth actions
- "Share" button on task detail page generates link (copies to clipboard)

### 7. Error Pages

- `/ui/403` — "Access denied. You need to be a member of [org] to access AltFix."
- `/ui/404` — "Task not found."
- `/ui/login-failed` — "GitHub login failed. [Try again]"
- `/ui/session-expired` — "Session expired. [Log in again]"

## File Structure

```
internal/daemon/web/
├── embed.go              # //go:embed all:templates all:static
├── web.go                # RegisterRoutes, templateFuncs, renderTemplate
├── handlers.go           # Dashboard, detail, new, PRs, settings handlers (~200 LoC)
├── auth.go               # GitHub OAuth flow, session management (~150 LoC)
├── share.go              # Shared URL generation, verification, redaction (~80 LoC)
├── middleware.go          # RequireAuth, RequireAdmin, CSRFCheck (~60 LoC)
├── session.go            # In-memory session store with TTL (~50 LoC)
├── templates/
│   ├── layout.html       # Base: htmx, tailwind, nav, dark mode, flash messages
│   ├── login.html        # GitHub OAuth button
│   ├── dashboard.html    # KPI cards + task list + filters
│   ├── detail.html       # Phase timeline + SSE feed + steering
│   ├── new.html          # Progressive task creation form
│   ├── prs.html          # PR tracker table
│   ├── settings.html     # Config viewer/editor
│   ├── share.html        # Public read-only detail
│   ├── partials/
│   │   ├── task_card.html      # Single task card (htmx swap target)
│   │   ├── task_list.html      # Full task list (polling refresh)
│   │   ├── event_item.html     # SSE event (type-specific rendering)
│   │   ├── kpi_cards.html      # Metrics row
│   │   ├── phase_bar.html      # Phase timeline
│   │   ├── repo_issues.html    # Issue dropdown (loaded on repo select)
│   │   └── pr_row.html         # Single PR table row
│   └── errors/
│       ├── 403.html
│       ├── 404.html
│       └── session_expired.html
└── static/
    ├── htmx.min.js       # Vendored htmx 2.x (~14KB gzipped)
    ├── sse.js             # htmx SSE extension (~2KB)
    ├── tailwind.css       # Vendored + purged (~20KB)
    └── app.css            # Status colors, feed layout, dark mode, animations (~100 lines)
```

**Estimated size**:

| Component | LoC | Notes |
|-----------|-----|-------|
| Go handlers | ~300 | 7 page handlers + partial renderers |
| Auth + session | ~250 | OAuth flow, session store, org cache |
| Share + redaction | ~150 | HMAC signing, verification, secret stripping |
| Middleware | ~100 | CSRF, auth check, admin check |
| Templates | ~1200 | 8 pages + 7 partials + 3 error pages + layout |
| CSS + JS | ~150 | Tailwind overrides, SSE script, dark mode |
| Tests | ~600 | Auth, CSRF, share, redaction, handler tests |
| **Total** | **~2750** | Codex estimated 3-5K; this is the tight end |

## Data Flow

### Dashboard polling
```
Browser → GET /ui/partials/task-list (every 5s, htmx)
Server  → store.ListTasks() → render task_list.html partial → HTML response
Browser → htmx swaps innerHTML of task list container
```

### Task detail SSE
```
Browser → EventSource /api/tasks/:id/sse (existing endpoint)
Server  → polls store.ListEvents(), sends SSE events
Server  → for each event, renders event_item.html partial as SSE data
Browser → htmx-sse appends HTML to #activity-feed
```

### Steering
```
Browser → POST /api/tasks/:id/steer (existing endpoint)
         hx-headers includes X-CSRF-Token
Server  → store.AppendEvent("user_steer", ...) → 202
Browser → form resets, event appears in SSE feed
```

### Shared view
```
Browser → GET /share/{taskID}.{hmac}?exp=...
Server  → verify HMAC, check expiry, check revocation
        → store.GetTask() + store.ListEvents()
        → redact secrets from events
        → render share.html (no nav, no auth, no steering)
```

## Security

### Threat Model

| Concern | Mitigation |
|---------|-----------|
| OAuth state hijack | `state` param + PKCE `code_verifier`, both session-bound |
| CSRF on POST forms | Double-submit cookie, verified in middleware |
| Session fixation | New session ID generated on login, old ID invalidated |
| Cookie theft | `HttpOnly`, `Secure`, `SameSite=Lax` |
| Unauthorized access | `allowed_orgs` / `allowed_users` checked on every request |
| Shared URL leaks | HMAC-signed, 24h expiry, revocable, data redacted |
| Secret exposure in events | `redactSecrets()` strips env vars, API keys, tokens from event data |
| XSS in event data | Go `html/template` auto-escapes all output |
| Unbounded DOM | Cap at 200 events, older collapsed behind "Show more" |
| Connection storm | Dashboard polls one endpoint, not N SSE per N tasks |
| Air-gapped deploy | All assets vendored in binary, no CDN calls |

### OAuth Callback Validation (Codex blocker)

```go
func (a *AuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
    // 1. Verify state param matches session-stored state (prevents CSRF on OAuth)
    // 2. Verify PKCE code_verifier stored in session matches
    // 3. Exchange code for token with GitHub API
    // 4. Fetch user info: GET /user and GET /user/orgs
    // 5. Check user login against allowed_users OR org membership against allowed_orgs
    // 6. On failure: redirect to /ui/login-failed with reason
    // 7. On success: rotate session ID (prevent fixation), set cookie, redirect /ui/
}
```

Failure paths: revoked GitHub token → 403 page with "re-authorize" link.
Denied org membership → 403 page with admin contact info.

### Org Membership Caching (CC blocker)

GitHub org membership checks must NOT hit GitHub API on every request (5000/hr limit).

```go
type OrgCache struct {
    mu    sync.RWMutex
    cache map[string]orgEntry // key: github username
}
type orgEntry struct {
    orgs     []string
    cachedAt time.Time
}
const orgCacheTTL = 15 * time.Minute
```

On login: fetch + cache. On subsequent requests: check cache. Refresh after TTL.

### Session Lifecycle (Codex blocker)

```
Login  → generate random session ID, store in server map, set cookie
Active → refresh TTL on each request (sliding window, 8h max)
Idle   → expire after 2h of no requests
Logout → delete from server map, clear cookie
Rotate → new session ID on login (prevent fixation)
Multi-tab → same session cookie, all tabs share session
Restart → all sessions lost (v1 acceptable, v2: persist to SQLite)
```

### CSRF Enforcement (Codex blocker)

Every POST/PUT/DELETE request (including htmx `hx-post`, `hx-delete`) must include
the CSRF token. Enforcement:

```go
func CSRFMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method == "GET" || r.Method == "HEAD" {
            next.ServeHTTP(w, r) // safe methods skip CSRF
            return
        }
        // Check X-CSRF-Token header (htmx sends via hx-headers)
        // OR check _csrf form field (non-htmx forms)
        token := r.Header.Get("X-CSRF-Token")
        if token == "" {
            token = r.FormValue("_csrf")
        }
        if !validateCSRFToken(r, token) {
            http.Error(w, "CSRF validation failed", 403)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

All templates inject token via layout: `<meta name="csrf-token" content="{{.CSRFToken}}">`.
htmx global config: `htmx.config.getCsrfToken = function() { return document.querySelector('meta[name=csrf-token]').content }`.

### HMAC Shared URL Signing (CC + Codex blocker)

Canonical signing format with separator to prevent concatenation ambiguity:

```go
// Sign: taskID + NUL separator + expiry (prevents "abc"+"123" == "ab"+"c123")
func signShareURL(taskID string, expiry int64, secret []byte) string {
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(taskID))
    mac.Write([]byte{0x00}) // NUL separator
    mac.Write([]byte(strconv.FormatInt(expiry, 10)))
    return hex.EncodeToString(mac.Sum(nil))
}

// Verify: recompute HMAC from URL params, check expiry, check revocation
func verifyShareURL(taskID, token string, expiry int64, secret []byte) error {
    if time.Now().Unix() > expiry {
        return errExpired
    }
    expected := signShareURL(taskID, expiry, secret)
    if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
        return errInvalidSignature
    }
    // Check revocation in shares table
    if isRevoked(taskID) {
        return errRevoked
    }
    return nil
}
```

Clock skew: allow 60s grace. Key rotation: support `secret` + `previous_secret`
(check both during rotation window).

### Secret Redaction Policy (Codex blocker)

Deterministic server-side redaction before rendering event data:

```go
var redactPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|auth)\s*[:=]\s*\S+`),
    regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),          // OpenAI/altllm keys
    regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),           // GitHub PATs
    regexp.MustCompile(`-----BEGIN [A-Z ]+ KEY-----`),    // PEM keys
    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),              // AWS access keys
}

func redactSecrets(data string) string {
    for _, re := range redactPatterns {
        data = re.ReplaceAllString(data, "[REDACTED]")
    }
    return data
}
```

Applied in: shared view rendering, event_item.html partial for shared URLs.
NOT applied in authenticated views (operators may need to see full output).

### Steering Authorization (CC blocker)

Steering burns budget and redirects agents. It is NOT a read-only action.

- **Admin users**: can steer any task
- **Viewer users**: can view tasks but NOT steer or stop
- Steering endpoint checks `session.IsAdmin` before allowing
- Every steer action is audit-logged: `store.AppendEvent(taskID, "user_steer", {user, message})`
- Non-admin max_cost override capped at daemon's per-task limit

## Dark Mode

Tailwind's `dark:` variant classes. Toggle stored in cookie. Default: system preference
via `prefers-color-scheme` media query.

```html
<html class="{{if .DarkMode}}dark{{end}}">
```

Status colors remain distinguishable in both modes (tested against WCAG contrast).

## Testing

### Unit tests (`internal/daemon/web/`)
- Auth flow: OAuth redirect, callback exchange, session creation, org check, logout
- CSRF: token generation, validation, rejection on mismatch
- Share: HMAC generation, verification, expiry check, revocation
- Middleware: auth required, admin required, session expired
- Redaction: secrets stripped from event data

### Integration tests
- Template rendering: each page renders without error with mock data
- SSE partial: event_item.html renders correctly for each event type
- Progressive form: repo select → issues load → submit creates task

### E2E (manual, tmux)
- Login → dashboard → create task → watch SSE → steer → verify in GitHub

## Dependencies

**Zero new Go dependencies.** Uses only stdlib:
- `html/template` — rendering
- `net/http` — handlers
- `embed` — static assets
- `crypto/hmac`, `crypto/sha256` — shared URLs
- `encoding/hex` — token encoding

GitHub OAuth uses `net/http` directly (no oauth2 library needed for a single provider).

## Migration Path

v1 (this spec): htmx dashboard, GitHub OAuth, core pages, shared URLs
v2: WebSocket upgrade for bidirectional steering (replace SSE for detail page only)
v3: Multi-daemon federation (central control plane for multiple daemon instances)

## Research Sources

This design incorporates findings from analysis of:
- **OpenHands Web** (56K LoC React): skip IDE complexity, steal status timeline pattern
- **OpenHands Eval Monitor** (9.4K LoC): StatusTimeline, auto-refresh, cost tracking
- **OpenHands Trajectory Visualizer**: type-specific event rendering
- **OpenHands PR Dashboard**: KPI cards, filter patterns
- **OpenHands CLI**: token streaming, confirmation gates
- **cloud-claw AltFix UI** (748 LoC React): activity feed, 3-mode form, steering UX
- **CC review**: CSRF, org allowlist, vendored Tailwind, multiplexed polling, diff viewer
- **Codex review**: PKCE, cookie flags, shared URL HMAC, DOM cap, progressive disclosure
