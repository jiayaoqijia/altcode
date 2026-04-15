package web

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// PageData carries data into every template render.
type PageData struct {
	Title     string
	ShowNav   bool
	DarkMode  bool
	CSRFToken string
	IsAdmin   bool
	User      *SessionUser
	Content   any
	BasePath  string
}

// Templates holds the parsed template set.
type Templates struct {
	pages  map[string]*template.Template
	funcs  template.FuncMap
	layout string
}

// templateFuncs returns the shared function map. The basePath
// parameter is captured in a closure by absPath so templates
// can write {{absPath "/ui/"}} instead of {{.BasePath}}/ui/.
func templateFuncs(basePath string) template.FuncMap {
	return template.FuncMap{
		"truncate":   truncate,
		"statusDot":  statusDot,
		"phaseClass": phaseClass,
		"absPath": func(path string) string {
			return basePath + path
		},
	}
}

// truncate shortens s to n runes, appending "..." if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}

// statusDot returns the CSS class for a status dot.
func statusDot(status string) string {
	switch status {
	case "pending":
		return "dot dot-pending"
	case "planning":
		return "dot dot-planning"
	case "implementing":
		return "dot dot-implementing"
	case "reviewing":
		return "dot dot-reviewing"
	case "testing":
		return "dot dot-testing"
	case "pr_open":
		return "dot dot-pr_open"
	case "merged":
		return "dot dot-merged"
	case "closed":
		return "dot dot-closed"
	case "failed":
		return "dot dot-failed"
	case "cancelled":
		return "dot dot-cancelled"
	default:
		return "dot dot-pending"
	}
}

// phaseClass returns the CSS class for a phase step relative
// to the current phase.
func phaseClass(current, phase string) string {
	order := []string{
		"pending", "planning", "implementing",
		"reviewing", "testing", "pr_open",
		"merged",
	}
	ci, pi := -1, -1
	for i, p := range order {
		if p == current {
			ci = i
		}
		if p == phase {
			pi = i
		}
	}
	if ci < 0 || pi < 0 {
		return "phase-pending"
	}
	switch {
	case pi < ci:
		return "phase-done"
	case pi == ci:
		return "phase-active"
	default:
		return "phase-pending"
	}
}

// LoadTemplates reads the embedded filesystem, parses layout.html,
// discovers partials, then builds each page template as
// layout + partials + page. Error pages are keyed as "error-NNN".
// basePath is captured by the absPath template function.
func LoadTemplates(basePath string) (*Templates, error) {
	funcs := templateFuncs(basePath)

	// Read layout.
	layoutBytes, err := fs.ReadFile(content, "templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("read layout: %w", err)
	}
	layoutStr := string(layoutBytes)

	// Collect partials.
	var partialStrs []string
	_ = fs.WalkDir(content, "templates/partials", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(p) != ".html" {
			return nil
		}
		b, rerr := fs.ReadFile(content, p)
		if rerr != nil {
			return nil
		}
		partialStrs = append(partialStrs, string(b))
		return nil
	})

	pages := make(map[string]*template.Template)

	// Walk top-level page templates.
	entries, err := fs.ReadDir(content, "templates")
	if err != nil {
		return nil, fmt.Errorf("read templates dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".html" {
			continue
		}
		if e.Name() == "layout.html" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".html")
		t, perr := parsePage(funcs, layoutStr, partialStrs, "templates/"+e.Name())
		if perr != nil {
			return nil, fmt.Errorf("parse page %s: %w", name, perr)
		}
		pages[name] = t
	}

	// Walk error templates.
	_ = fs.WalkDir(content, "templates/errors", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(p) != ".html" {
			return nil
		}
		base := strings.TrimSuffix(filepath.Base(p), ".html")
		name := "error-" + base
		t, perr := parsePage(funcs, layoutStr, partialStrs, p)
		if perr != nil {
			return nil
		}
		pages[name] = t
		return nil
	})

	return &Templates{pages: pages, funcs: funcs, layout: layoutStr}, nil
}

// parsePage creates one template from layout + partials + page file.
func parsePage(
	funcs template.FuncMap,
	layoutStr string,
	partialStrs []string,
	pagePath string,
) (*template.Template, error) {
	pageBytes, err := fs.ReadFile(content, pagePath)
	if err != nil {
		return nil, err
	}
	t := template.New("layout.html").Funcs(funcs)
	if _, err := t.Parse(layoutStr); err != nil {
		return nil, err
	}
	for _, ps := range partialStrs {
		if _, err := t.Parse(ps); err != nil {
			return nil, err
		}
	}
	if _, err := t.Parse(string(pageBytes)); err != nil {
		return nil, err
	}
	return t, nil
}

// Page returns the named page template, or nil if not found.
func (t *Templates) Page(name string) *template.Template {
	return t.pages[name]
}

// Names returns sorted page template names.
func (t *Templates) Names() []string {
	names := make([]string, 0, len(t.pages))
	for k := range t.pages {
		names = append(names, k)
	}
	return names
}

// ErrTemplateNotFound is returned when a named template does not exist.
var ErrTemplateNotFound = errors.New("template not found")

// Render executes a full-page template into w.
func Render(w http.ResponseWriter, t *Templates, name string, data PageData) error {
	page := t.pages[name]
	if page == nil {
		return fmt.Errorf("%w: %s", ErrTemplateNotFound, name)
	}
	var buf bytes.Buffer
	if err := page.Execute(&buf, data); err != nil {
		return fmt.Errorf("execute %q: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := buf.WriteTo(w)
	return err
}

// RenderPartial executes a named sub-template (partial) and writes
// the result to w. Useful for htmx partial responses.
func RenderPartial(
	w http.ResponseWriter,
	t *Templates,
	page string,
	partial string,
	data any,
) error {
	tmpl := t.pages[page]
	if tmpl == nil {
		return fmt.Errorf("%w: %s", ErrTemplateNotFound, page)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, partial, data); err != nil {
		return fmt.Errorf("execute partial %q in %q: %w", partial, page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := buf.WriteTo(w)
	return err
}

// ValidateBasePath checks that basePath is a clean URL path
// segment with no query params, fragments, percent-encoding,
// semicolons, or whitespace.
func ValidateBasePath(bp string) error {
	if bp == "" {
		return nil
	}
	if !strings.HasPrefix(bp, "/") {
		return fmt.Errorf("base path must start with /")
	}
	for _, ch := range bp {
		if ch == '%' || ch == ';' || ch == '?' ||
			ch == '#' || ch == ' ' || ch == '\t' {
			return fmt.Errorf(
				"base path contains invalid character %q", string(ch),
			)
		}
	}
	return nil
}

// RegisterRoutes mounts all web UI routes on the provided mux.
func RegisterRoutes(mux *http.ServeMux, cfg WebConfig) error {
	// Reject short signing keys — an HMAC with fewer than 32 bytes
	// is trivially brute-forceable.
	if len(cfg.SigningKey) > 0 && len(cfg.SigningKey) < 32 {
		return fmt.Errorf("signing key must be at least 32 bytes")
	}

	// Normalize and validate BasePath.
	cfg.BasePath = strings.TrimRight(cfg.BasePath, "/")
	if err := ValidateBasePath(cfg.BasePath); err != nil {
		return fmt.Errorf("invalid base path: %w", err)
	}

	tmpl, err := LoadTemplates(cfg.BasePath)
	if err != nil {
		return err
	}

	// Static assets.
	staticFS, _ := fs.Sub(content, "static")
	mux.Handle(
		"GET /ui/static/",
		http.StripPrefix("/ui/static/",
			http.FileServer(http.FS(staticFS))),
	)

	sessions := cfg.Sessions
	h := &WebHandler{
		tmpl:     tmpl,
		store:    cfg.Store,
		sessions: sessions,
		cfg:      cfg,
		orgCache: NewOrgCache(15 * time.Minute),
	}

	// Auth routes (no session required).
	if cfg.CloudMode {
		mux.HandleFunc(
			"GET /auth/session", h.HandleSessionTicket,
		)
		// In cloud mode, GitHub OAuth is not used.
		mux.HandleFunc("GET /auth/github", func(
			w http.ResponseWriter, r *http.Request,
		) {
			http.NotFound(w, r)
		})
	} else {
		mux.HandleFunc(
			"GET /auth/github", h.HandleOAuthRedirect,
		)
	}
	mux.HandleFunc("GET /auth/callback", h.HandleOAuthCallback)
	mux.HandleFunc("GET /ui/login", h.HandleLoginPage)

	// Test-mode auth bypass (never in production).
	if cfg.TestMode {
		log.Println(
			"WARNING: test mode enabled — " +
				"/auth/test-login bypass is active. " +
				"DO NOT use in production.",
		)
		mux.HandleFunc(
			"GET /auth/test-login", h.HandleTestLogin,
		)
	}

	// Shared view (no session required).
	mux.HandleFunc("GET /share/{token}", h.HandleShareView)

	// Auth + CSRF middleware.
	auth := RequireAuth(sessions, cfg.BasePath)
	csrf := CSRFCheck()

	// Authenticated page routes.
	mux.Handle(
		"GET /ui/",
		auth(http.HandlerFunc(h.HandleDashboard)),
	)
	mux.Handle(
		"GET /ui/tasks/new",
		auth(http.HandlerFunc(h.HandleNewTask)),
	)
	mux.Handle(
		"GET /ui/tasks/{id}",
		auth(http.HandlerFunc(h.HandleTaskDetail)),
	)
	mux.Handle(
		"GET /ui/prs",
		auth(http.HandlerFunc(h.HandlePRs)),
	)
	mux.Handle(
		"GET /ui/settings",
		auth(http.HandlerFunc(h.HandleSettings)),
	)
	mux.Handle(
		"GET /ui/tasks/{id}/events",
		auth(http.HandlerFunc(h.HandleSSEHTML)),
	)

	// Partial endpoints for htmx polling.
	mux.Handle(
		"GET /ui/partials/task-list",
		auth(http.HandlerFunc(h.HandlePartialTaskList)),
	)
	mux.Handle(
		"GET /ui/partials/kpi-cards",
		auth(http.HandlerFunc(h.HandlePartialKPICards)),
	)
	mux.Handle(
		"GET /ui/partials/repo-issues",
		auth(http.HandlerFunc(h.HandlePartialRepoIssues)),
	)

	mux.Handle(
		"GET /ui/partials/pr-list",
		auth(http.HandlerFunc(h.HandlePartialPRList)),
	)

	// Mutating routes with CSRF.
	mux.Handle(
		"POST /auth/logout",
		auth(csrf(http.HandlerFunc(h.HandleLogout))),
	)

	return nil
}
