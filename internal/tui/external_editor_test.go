package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPickEditor_HonorsEnvVars ensures $VISUAL beats $EDITOR (Unix
// convention) and that an empty env falls back to a candidate.
func TestPickEditor_HonorsEnvVars(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("editor selection differs on windows")
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	t.Setenv("EDITOR", "nano-test-stub")
	got := pickEditor()
	if got != "nano-test-stub" {
		t.Errorf("EDITOR not honoured: got %q", got)
	}

	t.Setenv("VISUAL", "vim-test-stub")
	got = pickEditor()
	if got != "vim-test-stub" {
		t.Errorf("VISUAL should beat EDITOR: got %q", got)
	}
}

// TestOpenInExternalEditor_RoundtripsEditedContent uses a tiny shell
// helper as the "editor" — it overwrites the file with KNOWN content,
// proving openInExternalEditor reads the post-edit text and not the
// initial seed.
func TestOpenInExternalEditor_RoundtripsEditedContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("editor stub uses sh")
	}

	// Build a tiny shell script that overwrites $1 with "edited!".
	dir := t.TempDir()
	stub := filepath.Join(dir, "fake-editor")
	script := `#!/bin/sh
echo "edited!" > "$1"
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", stub)
	t.Setenv("EDITOR", "")

	got, err := openInExternalEditor("seed text")
	if err != nil {
		t.Fatalf("openInExternalEditor: %v", err)
	}
	if !strings.Contains(got, "edited!") {
		t.Errorf("got %q, want to contain 'edited!'", got)
	}
}

// TestOpenInExternalEditor_PreservesSeedOnNoChange covers the user
// who opens the editor and quits without saving — the seed should
// come back unchanged.
func TestOpenInExternalEditor_PreservesSeedOnNoChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("editor stub uses sh")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "fake-editor-noop")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", stub)
	t.Setenv("EDITOR", "")

	got, err := openInExternalEditor("untouched seed")
	if err != nil {
		t.Fatalf("openInExternalEditor: %v", err)
	}
	if !strings.Contains(got, "untouched seed") {
		t.Errorf("got %q, want seed to round-trip", got)
	}
}
