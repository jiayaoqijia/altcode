package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectLanguage_RecognizesEachManifest covers the full switch in
// detectLanguage. We create one fake manifest per language and verify
// the detector returns the matching label.
func TestDetectLanguage_RecognizesEachManifest(t *testing.T) {
	cases := map[string]string{
		"go.mod":           "Go",
		"package.json":     "TypeScript",
		"Cargo.toml":       "Rust",
		"pyproject.toml":   "Python",
		"requirements.txt": "Python",
		"pom.xml":          "Java",
	}
	for file, want := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, file), []byte("// stub"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		got := detectLanguage(dir)
		if got != want {
			t.Errorf("detectLanguage(%s) = %q, want %q", file, got, want)
		}
	}
}

// TestDetectLanguage_UnknownEmptyDir returns "Unknown" when no manifest
// is present. Anchors the fallback branch.
func TestDetectLanguage_UnknownEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if got := detectLanguage(dir); got != "Unknown" {
		t.Errorf("empty dir detectLanguage = %q, want Unknown", got)
	}
}

// TestDetectLanguage_FirstMatchWins ensures the lookup order from the
// implementation is preserved: a Go project that also has a node_modules
// (eg. a Tailwind frontend) is still classified as Go because go.mod
// is checked first.
func TestDetectLanguage_FirstMatchWins(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectLanguage(dir); got != "Go" {
		t.Errorf("Go+JS dir = %q, want Go (priority order)", got)
	}
}

// TestBuildClaudeMD_GoProject covers the Go branch of buildClaudeMD plus
// the directory-listing path. We seed a tempdir with go.mod + a few
// real subdirs and one each of vendor/dot/node_modules to confirm the
// filter works.
func TestBuildClaudeMD_GoProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"cmd", "internal", "vendor", ".git", "node_modules", "dist"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := buildClaudeMD(dir)

	// Header + language section.
	if !strings.Contains(got, "# Project Guide") {
		t.Error("missing project header")
	}
	if !strings.Contains(got, "## Language: Go") {
		t.Error("language not detected as Go")
	}
	// Build commands for Go.
	if !strings.Contains(got, "go build ./...") {
		t.Error("missing Go build command")
	}
	// Listed dirs (must include cmd/internal).
	if !strings.Contains(got, "cmd/") || !strings.Contains(got, "internal/") {
		t.Error("missing cmd/ or internal/ in structure")
	}
	// Filtered dirs (must NOT include vendor/.git/node_modules/dist).
	for _, banned := range []string{"vendor/", ".git/", "node_modules/", "dist/"} {
		if strings.Contains(got, banned) {
			t.Errorf("structure should not contain %q", banned)
		}
	}
	// Hard rules section.
	if !strings.Contains(got, "Never commit secrets") {
		t.Error("missing hard rules section")
	}
}

// TestBuildClaudeMD_LanguageBranches walks every language case to
// exercise the per-language build-command switch.
func TestBuildClaudeMD_LanguageBranches(t *testing.T) {
	cases := []struct {
		manifest, marker string
	}{
		{"go.mod", "go build ./..."},
		{"package.json", "npm install"},
		{"pyproject.toml", "pytest"},
		{"requirements.txt", "pytest"},
		{"Cargo.toml", "cargo build"},
		{"pom.xml", "# Add your build commands here"}, // Java falls through to default
	}
	for _, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, c.manifest), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := buildClaudeMD(dir)
		if !strings.Contains(got, c.marker) {
			t.Errorf("manifest=%s: missing marker %q in:\n%s", c.manifest, c.marker, got)
		}
	}
}

// TestBuildClaudeMD_UnknownLangAndEmptyStructure exercises the fallthrough
// build-command branch and the "no directories" message.
func TestBuildClaudeMD_UnknownLangAndEmptyStructure(t *testing.T) {
	dir := t.TempDir()
	got := buildClaudeMD(dir)
	if !strings.Contains(got, "## Language: Unknown") {
		t.Error("expected Unknown language header")
	}
	if !strings.Contains(got, "# Add your build commands here") {
		t.Error("expected fallback build comment")
	}
	if !strings.Contains(got, "(no directories detected)") {
		t.Error("expected empty-structure marker")
	}
}

// TestBuildClaudeMD_TruncatesAt30Dirs proves the 30-directory cap fires
// without listing every entry.
func TestBuildClaudeMD_TruncatesAt30Dirs(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 35; i++ {
		_ = os.Mkdir(filepath.Join(dir, "d"+string(rune('a'+i%26))+string(rune('a'+i/26))), 0o755)
	}
	got := buildClaudeMD(dir)
	if !strings.Contains(got, "...\n") {
		t.Error("expected truncation marker after 30 entries")
	}
}

// TestRunInit_WritesFileAndReportsBytes drives the happy path of /init.
func TestRunInit_WritesFileAndReportsBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	a := testApp()
	if cmd := a.runInit(); cmd != nil {
		// runInit returns nil — exercise but don't drive.
		_ = cmd
	}

	out := filepath.Join(dir, "CLAUDE.md")
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("CLAUDE.md not written: %v", err)
	}
	if !strings.Contains(string(body), "## Language: Go") {
		t.Error("written CLAUDE.md missing language header")
	}
}

// TestRunInit_RefusesOverwrite verifies the "already exists" guard.
func TestRunInit_RefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(existing, []byte("# preserved"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	a := testApp()
	a.runInit()

	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("CLAUDE.md disappeared: %v", err)
	}
	if string(body) != "# preserved" {
		t.Errorf("CLAUDE.md was overwritten: %q", string(body))
	}
}

// TestRunDoctor_NilEngineDoesNotPanic exercises the most fragile path —
// /doctor was the source of multiple historical panics when the engine
// wasn't fully constructed yet.
func TestRunDoctor_NilEngineDoesNotPanic(t *testing.T) {
	a := testApp() // engine is nil in test fixture
	out := a.runDoctor()
	if !strings.Contains(out, "Doctor Report") {
		t.Errorf("missing doctor header: %q", out)
	}
	// The nil-engine path should report tools as not initialized.
	if !strings.Contains(out, "engine not initialized") {
		t.Errorf("missing nil-engine tools fallback: %q", out)
	}
	// Git + agents sections always run regardless of engine state.
	if !strings.Contains(out, "Git:") {
		t.Errorf("missing git probe: %q", out)
	}
	if !strings.Contains(out, "Agents:") {
		t.Errorf("missing agents probe: %q", out)
	}
}
