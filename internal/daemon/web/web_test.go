package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadTemplates(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	if tmpl == nil {
		t.Fatal("LoadTemplates returned nil")
	}
	// login.html should be loaded as "login".
	if tmpl.Page("login") == nil {
		t.Error("expected 'login' page template")
	}
	names := tmpl.Names()
	if len(names) == 0 {
		t.Error("expected at least one page template")
	}
}

func TestRender(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	w := httptest.NewRecorder()
	data := PageData{
		Title:     "Login",
		CSRFToken: "tok123",
		DarkMode:  false,
		ShowNav:   false,
	}
	if err := Render(w, tmpl, "login", data); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Login") {
		t.Error("expected body to contain 'Login'")
	}
	if !strings.Contains(body, "tok123") {
		t.Error("expected body to contain CSRF token")
	}
	if !strings.Contains(body, "AltFix") {
		t.Error("expected body to contain 'AltFix'")
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
}

func TestRenderWithNav(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	w := httptest.NewRecorder()
	data := PageData{
		Title:   "Dashboard",
		ShowNav: true,
		User: &SessionUser{
			Login:   "octocat",
			IsAdmin: true,
		},
		CSRFToken: "csrf-abc",
	}
	if err := Render(w, tmpl, "login", data); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "octocat") {
		t.Error("expected nav to show username")
	}
	if !strings.Contains(body, "/settings") {
		t.Error("expected admin nav to show Settings link")
	}
	if !strings.Contains(body, "Logout") {
		t.Error("expected nav to show Logout")
	}
}

func TestRenderDarkMode(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	w := httptest.NewRecorder()
	data := PageData{
		Title:    "Login",
		DarkMode: true,
	}
	if err := Render(w, tmpl, "login", data); err != nil {
		t.Fatalf("Render: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `class="dark"`) {
		t.Error("expected dark class on html element")
	}
}

func TestRenderNotFound(t *testing.T) {
	tmpl, err := LoadTemplates()
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	w := httptest.NewRecorder()
	err = Render(w, tmpl, "nonexistent", PageData{})
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"ab", 1, "a"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q",
				tt.s, tt.n, got, tt.want)
		}
	}
}

func TestStatusDot(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"pending", "dot dot-pending"},
		{"planning", "dot dot-planning"},
		{"implementing", "dot dot-implementing"},
		{"merged", "dot dot-merged"},
		{"failed", "dot dot-failed"},
		{"unknown", "dot dot-pending"},
	}
	for _, tt := range tests {
		got := statusDot(tt.status)
		if got != tt.want {
			t.Errorf("statusDot(%q) = %q, want %q",
				tt.status, got, tt.want)
		}
	}
}

func TestPhaseClass(t *testing.T) {
	tests := []struct {
		current string
		phase   string
		want    string
	}{
		{"implementing", "planning", "phase-done"},
		{"implementing", "implementing", "phase-active"},
		{"implementing", "reviewing", "phase-pending"},
		{"merged", "pending", "phase-done"},
		{"pending", "merged", "phase-pending"},
		{"unknown", "planning", "phase-pending"},
	}
	for _, tt := range tests {
		got := phaseClass(tt.current, tt.phase)
		if got != tt.want {
			t.Errorf("phaseClass(%q, %q) = %q, want %q",
				tt.current, tt.phase, got, tt.want)
		}
	}
}
