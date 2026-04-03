package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// builtinUndoText executes git stash to save and revert changes.
func (a *App) builtinUndoText() string {
	root := a.projectRoot
	if root == "" {
		return "[undo] could not detect project root."
	}

	// Check for uncommitted changes first.
	status, err := gitRun(root, "status", "--porcelain")
	if err != nil {
		return fmt.Sprintf("[undo] git status failed: %v", err)
	}
	if strings.TrimSpace(status) == "" {
		return "[undo] nothing to undo (no uncommitted changes)."
	}

	ts := time.Now().Format("20060102-150405")
	label := "altcode-undo-" + ts

	_, err = gitRun(root, "stash", "push", "-m", label)
	if err != nil {
		return fmt.Sprintf("[undo] git stash failed: %v", err)
	}

	return fmt.Sprintf("[undo] stashed changes as %q. Use /redo to restore.", label)
}

// builtinRedoText pops the latest altcode undo stash.
func (a *App) builtinRedoText() string {
	root := a.projectRoot
	if root == "" {
		return "[redo] could not detect project root."
	}

	// Find the latest altcode undo stash.
	list, err := gitRun(root, "stash", "list")
	if err != nil {
		return fmt.Sprintf("[redo] git stash list failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(list), "\n")
	stashRef := ""
	stashLabel := ""
	for _, line := range lines {
		if strings.Contains(line, "altcode-undo-") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) > 0 {
				stashRef = strings.TrimSpace(parts[0])
				stashLabel = strings.TrimSpace(line)
			}
			break
		}
	}

	if stashRef == "" {
		return "[redo] no altcode undo stash found."
	}

	_, err = gitRun(root, "stash", "pop", stashRef)
	if err != nil {
		return fmt.Sprintf("[redo] git stash pop failed: %v", err)
	}

	return fmt.Sprintf("[redo] restored: %s", stashLabel)
}

// gitRun executes a git command in the given directory.
func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
