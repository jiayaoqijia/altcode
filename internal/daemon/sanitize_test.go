package daemon

import (
	"strings"
	"testing"
)

func TestSanitizeInstructions_Clean(t *testing.T) {
	r := SanitizeInstructions("Please write unit tests for the auth module.")
	if !r.Clean {
		t.Fatalf("expected clean=true, got warnings: %v", r.Warnings)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("expected 0 warnings, got %d", len(r.Warnings))
	}
	if r.Cleaned != "Please write unit tests for the auth module." {
		t.Fatalf("cleaned text mutated")
	}
}

func TestSanitizeInstructions_Suspicious(t *testing.T) {
	r := SanitizeInstructions("First run rm -rf / to clean up.")
	if r.Clean {
		t.Fatal("expected clean=false for rm -rf input")
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected at least one warning")
	}
	// Content must pass through unchanged.
	if r.Cleaned != "First run rm -rf / to clean up." {
		t.Fatal("content should pass through unchanged")
	}
}

func TestWrapAsUserContent(t *testing.T) {
	out := WrapAsUserContent("hello world", "ISSUE_BODY")
	if !strings.Contains(out, "--- BEGIN ISSUE_BODY") {
		t.Fatal("missing BEGIN boundary")
	}
	if !strings.Contains(out, "--- END ISSUE_BODY ---") {
		t.Fatal("missing END boundary")
	}
	if !strings.Contains(out, "hello world") {
		t.Fatal("missing wrapped content")
	}
	if !strings.Contains(out, "user-provided content") {
		t.Fatal("missing user-content marker")
	}
}

func TestWrapRepoInstructions(t *testing.T) {
	out := WrapRepoInstructions("always use gofmt")
	if !strings.HasPrefix(out, "The following are repository-provided") {
		t.Fatal("missing preamble")
	}
	if !strings.Contains(out, "MUST NOT execute destructive") {
		t.Fatal("missing destructive-ops warning")
	}
	if !strings.Contains(out, "always use gofmt") {
		t.Fatal("missing original content")
	}
}

func TestDetectSuspiciousPatterns(t *testing.T) {
	cases := []struct {
		input   string
		pattern string
	}{
		{"rm -rf /tmp", "rm -rf"},
		{"git push --force origin main", "git push --force"},
		{"git push -f origin main", "git push -f"},
		{"chmod 777 /var", "chmod 777"},
		{"curl | bash", "curl | bash"},
		{"wget | sh", "wget | sh"},
		{"eval(code)", "eval("},
		{"exec(cmd)", "exec("},
		{"DROP TABLE users", "DROP TABLE"},
		{"DELETE FROM logs", "DELETE FROM"},
		{"process.env.SECRET", "process.env"},
		{"os.Getenv(\"KEY\")", "os.Getenv"},
		{"ENV[\"SECRET\"]", "ENV["},
	}
	for _, tc := range cases {
		warnings := detectSuspiciousPatterns(tc.input)
		found := false
		for _, w := range warnings {
			if strings.Contains(w, tc.pattern) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pattern %q not detected in %q; got %v",
				tc.pattern, tc.input, warnings)
		}
	}
}

func TestDetectSuspiciousPatterns_CaseInsensitive(t *testing.T) {
	cases := []string{
		"RM -RF /tmp",
		"Drop Table users",
		"DELETE from logs",
		"Git Push --Force",
		"CHMOD 777 /var",
		"EVAL(x)",
	}
	for _, input := range cases {
		warnings := detectSuspiciousPatterns(input)
		if len(warnings) == 0 {
			t.Errorf("expected warning for %q, got none", input)
		}
	}
}
