package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// builtinUndoText reverts the last file operations altcode performed in
// this session. It targets ONLY files the agent touched (tracked via
// the history journal) so a bag of unrelated working-tree changes
// from the user's own editor don't get clobbered by git stash.
func (a *App) builtinUndoText() string {
	root := a.projectRoot
	if root == "" {
		return "[undo] could not detect project root."
	}
	if a.engine == nil || a.engine.FileJournal() == nil {
		return "[undo] no file journal available."
	}
	entries := a.engine.FileJournal().Entries()
	if len(entries) == 0 {
		return "[undo] nothing to undo (altcode hasn't modified any files this session)."
	}

	// Collect unique agent-touched paths (journal may have duplicates
	// for write+edit on the same file).
	agentFiles := map[string]bool{}
	for _, e := range entries {
		if e.Path != "" && (e.Tool == "write" || e.Tool == "edit") {
			agentFiles[e.Path] = true
		}
	}
	if len(agentFiles) == 0 {
		return "[undo] nothing to undo (journal has no write/edit entries)."
	}

	ts := time.Now().Format("20060102-150405")
	label := "altcode-undo-" + ts

	// Safety: bail if the working tree has uncommitted changes to files
	// altcode DIDN'T touch — the old `git stash push -u` would scoop
	// them up silently and surprise the user. We only stash the paths
	// listed in agentFiles; anything else is the user's own work and
	// we refuse to trample it.
	status, err := gitRun(root, "status", "--porcelain")
	if err != nil {
		return fmt.Sprintf("[undo] git status failed: %v", err)
	}
	stashPrefix := []string{"stash", "push", "-u", "-m", label, "--"}
	stashArgs := append([]string{}, stashPrefix...)
	prefixLen := len(stashPrefix)
	for _, line := range strings.Split(strings.TrimSpace(status), "\n") {
		if line == "" {
			continue
		}
		// Each porcelain line looks like "XY path" (3-char prefix).
		if len(line) < 4 {
			continue
		}
		// Renamed files appear as "R  old -> new" — without the
		// rename split, the matcher saw a path like "old -> new" and
		// never matched the agent-touched basename, leaving
		// rename-only changes un-undoable. Split on " -> " and add
		// BOTH paths so the stash call covers either name.
		p := strings.TrimSpace(line[3:])
		if idx := strings.Index(p, " -> "); idx >= 0 {
			oldPath := strings.TrimSpace(p[:idx])
			newPath := strings.TrimSpace(p[idx+4:])
			if agentFiles[oldPath] || agentFiles[newPath] {
				stashArgs = append(stashArgs, oldPath, newPath)
			}
			continue
		}
		if agentFiles[p] {
			stashArgs = append(stashArgs, p)
		}
	}
	stashed := len(stashArgs) - prefixLen
	if stashed == 0 {
		return "[undo] nothing to undo (altcode's files have no uncommitted changes; the file may already be committed or live outside the git repo)."
	}

	if _, err := gitRun(root, stashArgs...); err != nil {
		return fmt.Sprintf("[undo] git stash failed: %v", err)
	}
	// Compute the count from prefixLen, not the magic number 5 — the
	// previous "len(stashArgs)-5" was off by one because the prefix
	// has 6 elements (push -u -m LABEL --), so a 1-file undo
	// reported "stashed 2 agent-modified file(s)".
	return fmt.Sprintf("[undo] stashed %d agent-modified file(s) as %q. Use /redo to restore.", stashed, label)
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

// gitRun executes a git command in the given directory with a 10s timeout.
func gitRun(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second // kill child processes on timeout
	out, err := cmd.CombinedOutput()
	return string(out), err
}
