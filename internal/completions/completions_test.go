package completions

import (
	"os"
	"path/filepath"
	"testing"
)

// setupTree creates a temporary directory tree for testing.
func setupTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	dirs := []string{
		"src",
		"src/util",
		".git",
		"node_modules/pkg",
		"vendor/lib",
	}
	files := []string{
		"main.go",
		"README.md",
		"src/app.go",
		"src/app_test.go",
		"src/util/helper.go",
		".gitignore",
		".git/HEAD",
		"node_modules/pkg/index.js",
		"vendor/lib/lib.go",
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCompleteEmptyQuery(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "", 100)

	if len(matches) == 0 {
		t.Fatal("expected matches for empty query")
	}

	// Should include visible files/dirs but not hidden or skip dirs.
	paths := matchPaths(matches)
	assertContains(t, paths, "main.go")
	assertContains(t, paths, "README.md")
	assertNotContains(t, paths, ".git")
	assertNotContains(t, paths, ".gitignore")
}

func TestCompleteSkipsDirs(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "", 100)
	paths := matchPaths(matches)

	// Files inside skipped dirs should not appear.
	for _, p := range paths {
		if filepath.HasPrefix(p, "node_modules") {
			t.Errorf("should skip node_modules, got %q", p)
		}
		if filepath.HasPrefix(p, "vendor") {
			t.Errorf("should skip vendor, got %q", p)
		}
		if filepath.HasPrefix(p, ".git") {
			t.Errorf("should skip .git, got %q", p)
		}
	}
}

func TestCompleteExactMatch(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "main.go", 10)

	if len(matches) == 0 {
		t.Fatal("expected match for 'main.go'")
	}
	if matches[0].Path != "main.go" {
		t.Errorf("top match = %q, want main.go", matches[0].Path)
	}
	if matches[0].Score != 3.0 {
		t.Errorf("exact match score = %f, want 3.0", matches[0].Score)
	}
}

func TestCompletePrefixMatch(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "main", 10)

	if len(matches) == 0 {
		t.Fatal("expected match for 'main'")
	}
	if matches[0].Score < 2.0 {
		t.Errorf("prefix score = %f, want >= 2.0", matches[0].Score)
	}
}

func TestCompleteSubstringMatch(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "app", 10)

	if len(matches) < 2 {
		t.Fatalf("expected >= 2 matches for 'app', got %d", len(matches))
	}
}

func TestCompleteFuzzyMatch(t *testing.T) {
	root := setupTree(t)
	// "hlpr" should fuzzy-match "helper.go" (h-l-p-r subsequence).
	matches := Complete(root, "hlpr", 10)

	found := false
	for _, m := range matches {
		if filepath.Base(m.Path) == "helper.go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected fuzzy match for helper.go with query 'hlpr'")
	}
}

func TestCompleteMaxResults(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "", 2)

	if len(matches) > 2 {
		t.Errorf("expected <= 2 results, got %d", len(matches))
	}
}

func TestCompleteDefaultMaxResults(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "", 0) // 0 should default to 20
	if len(matches) > 20 {
		t.Errorf("expected <= 20 results with default, got %d", len(matches))
	}
}

func TestCompleteIsDirFlag(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "src", 10)

	for _, m := range matches {
		if m.Path == "src" && !m.IsDir {
			t.Error("src should be marked as directory")
		}
		if m.Path == "main.go" && m.IsDir {
			t.Error("main.go should not be marked as directory")
		}
	}
}

func TestCompleteCaseInsensitive(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "README", 10)

	found := false
	for _, m := range matches {
		if filepath.Base(m.Path) == "README.md" {
			found = true
			break
		}
	}
	if !found {
		t.Error("case-insensitive match for README.md failed")
	}
}

func TestCompleteNoMatch(t *testing.T) {
	root := setupTree(t)
	matches := Complete(root, "zzzznonexistent", 10)

	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestScoreFunction(t *testing.T) {
	cases := []struct {
		path  string
		query string
		min   float64
	}{
		{"main.go", "main.go", 3.0},
		{"main.go", "main", 2.0},
		{"src/main.go", "main", 1.0},
		{"src/util/helper.go", "hlpr", 0.5},
		{"readme.md", "zzz", 0},
	}
	for _, tc := range cases {
		got := score(tc.path, tc.query)
		if got < tc.min {
			t.Errorf("score(%q, %q) = %f, want >= %f",
				tc.path, tc.query, got, tc.min)
		}
	}
}

func TestFuzzyMatch(t *testing.T) {
	if !fuzzyMatch("helper.go", "hlpr") {
		t.Error("expected fuzzyMatch('helper.go', 'hlpr') = true")
	}
	if fuzzyMatch("hello", "xyz") {
		t.Error("expected fuzzyMatch('hello', 'xyz') = false")
	}
	if !fuzzyMatch("abcdef", "") {
		t.Error("empty query should match everything")
	}
}

func TestIsHidden(t *testing.T) {
	if !isHidden(".git") {
		t.Error(".git should be hidden")
	}
	if isHidden("main.go") {
		t.Error("main.go should not be hidden")
	}
	if isHidden(".") {
		t.Error("'.' alone should not be considered hidden")
	}
}

// --- helpers ---

func matchPaths(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Path
	}
	return out
}

func assertContains(t *testing.T, paths []string, want string) {
	t.Helper()
	for _, p := range paths {
		if p == want {
			return
		}
	}
	t.Errorf("expected %q in results", want)
}

func assertNotContains(t *testing.T, paths []string, bad string) {
	t.Helper()
	for _, p := range paths {
		if p == bad {
			t.Errorf("did not expect %q in results", bad)
			return
		}
	}
}
