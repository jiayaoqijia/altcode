package tui

import (
	"os/exec"
	"strings"
	"testing"
)

// TestRunGit_OutsideRepo returns an error when not in a git tree.
func TestRunGit_OutsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	t.Chdir(dir)
	_, err := runGit("rev-parse", "--git-dir")
	if err == nil {
		t.Error("expected error outside repo")
	}
}

// TestRunGit_InsideRepo returns stdout for a known query.
func TestRunGit_InsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	out, err := runGit("rev-parse", "--show-toplevel")
	if err != nil {
		t.Skipf("not in a repo: %v", err)
	}
	if !strings.Contains(out, "altcode") {
		t.Errorf("expected toplevel to contain 'altcode': %q", out)
	}
}

// TestBuiltinDiffJournalFallback degrades cleanly when no engine /
// journal is wired (the fallback path that fires when git is absent).
func TestBuiltinDiffJournalFallback(t *testing.T) {
	a := testApp() // engine is nil in test fixture
	got := a.builtinDiffJournalFallback()
	if !strings.Contains(got, "No file history available") {
		t.Errorf("nil-engine fallback = %q", got)
	}
}
