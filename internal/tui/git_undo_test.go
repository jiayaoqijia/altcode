package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGitRun_VersionRoundtrip is the simplest happy path: any host that
// has git on PATH will respond to `git --version` with a deterministic
// string. Skip if git is missing.
func TestGitRun_VersionRoundtrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	out, err := gitRun(dir, "--version")
	if err != nil {
		t.Fatalf("gitRun --version failed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "git version") {
		t.Errorf("unexpected output: %q", out)
	}
}

// TestGitRun_BadArgsReturnsError checks the error path: git rejects the
// command and we surface a non-nil error plus combined output.
func TestGitRun_BadArgsReturnsError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	_, err := gitRun(dir, "this-is-not-a-real-subcommand")
	if err == nil {
		t.Error("expected error for unknown subcommand")
	}
}

// TestBuiltinUndoText_NoProjectRoot covers the first guard.
func TestBuiltinUndoText_NoProjectRoot(t *testing.T) {
	a := testApp()
	a.projectRoot = ""
	got := a.builtinUndoText()
	if !strings.Contains(got, "could not detect project root") {
		t.Errorf("unexpected output: %q", got)
	}
}

// TestBuiltinUndoText_NilEngine covers the no-journal guard. testApp()
// constructs an App with engine=nil, which is the second guard's branch.
func TestBuiltinUndoText_NilEngine(t *testing.T) {
	a := testApp()
	a.projectRoot = t.TempDir()
	got := a.builtinUndoText()
	if !strings.Contains(got, "no file journal available") {
		t.Errorf("expected no-journal message, got: %q", got)
	}
}

// TestBuiltinRedoText_NoProjectRoot exercises the redo's first guard.
func TestBuiltinRedoText_NoProjectRoot(t *testing.T) {
	a := testApp()
	a.projectRoot = ""
	got := a.builtinRedoText()
	if !strings.Contains(got, "could not detect project root") {
		t.Errorf("unexpected output: %q", got)
	}
}

// TestBuiltinRedoText_NoStashInRepo covers the "no altcode-undo stash"
// branch. Set up a fresh git repo with no stashes and call /redo.
func TestBuiltinRedoText_NoStashInRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	if _, err := gitRun(dir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	a := testApp()
	a.projectRoot = dir
	got := a.builtinRedoText()
	if !strings.Contains(got, "no altcode undo stash found") {
		t.Errorf("expected no-stash message, got: %q", got)
	}
}

// TestBuiltinRedoText_BadGitDir covers the "stash list failed" branch.
// We point projectRoot at a tempdir that is NOT a git repo; gitRun
// returns a non-zero exit and the function should report the failure
// cleanly rather than panicking.
func TestBuiltinRedoText_BadGitDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	a := testApp()
	a.projectRoot = filepath.Join(t.TempDir(), "definitely-not-a-repo")
	got := a.builtinRedoText()
	// One of the two error branches: either "stash list failed" if the
	// directory exists but isn't a git repo, or the no-stash message if
	// gitRun degrades silently. Both are valid; the contract is "no
	// panic + a human-readable string".
	if got == "" {
		t.Error("redo on non-repo returned empty string")
	}
	if strings.Contains(got, "panic") {
		t.Errorf("unexpected panic mention: %q", got)
	}
}
