// Package history tracks file operations with before/after snapshots.
package history

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Entry records a single file operation.
type Entry struct {
	Timestamp time.Time
	Tool      string // "write", "edit", "bash"
	Path      string
	Action    string // "create", "modify", "delete"
	Before    string // content before (empty for create)
	After     string // content after (empty for delete)
}

// Journal accumulates file operation entries.
type Journal struct {
	entries []Entry
	mu      sync.Mutex
}

// NewJournal creates an empty Journal.
func NewJournal() *Journal {
	return &Journal{}
}

// Record adds a file operation entry to the journal.
func (j *Journal) Record(tool, path, action, before, after string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, Entry{
		Timestamp: time.Now(),
		Tool:      tool,
		Path:      path,
		Action:    action,
		Before:    before,
		After:     after,
	})
}

// Entries returns a copy of all recorded entries.
func (j *Journal) Entries() []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	cp := make([]Entry, len(j.entries))
	copy(cp, j.entries)
	return cp
}

// Summary returns a human-readable summary of operations.
func (j *Journal) Summary() string {
	j.mu.Lock()
	defer j.mu.Unlock()

	counts := j.countActions()
	return buildSummary(counts)
}

// countActions tallies entries by action type (must hold lock).
func (j *Journal) countActions() map[string]int {
	counts := make(map[string]int)
	for _, e := range j.entries {
		counts[e.Action]++
	}
	return counts
}

func buildSummary(counts map[string]int) string {
	if len(counts) == 0 {
		return "no file operations"
	}
	var parts []string
	for _, a := range []string{"modified", "created", "deleted"} {
		if n, ok := counts[mapAction(a)]; ok && n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, a))
		}
	}
	if len(parts) == 0 {
		return "no file operations"
	}
	return strings.Join(parts, ", ")
}

// mapAction converts display labels back to action keys.
func mapAction(display string) string {
	switch display {
	case "modified":
		return "modify"
	case "created":
		return "create"
	case "deleted":
		return "delete"
	}
	return display
}

// Diff returns a unified diff for a given file path.
// Uses the most recent entry for that path.
func (j *Journal) Diff(path string) string {
	j.mu.Lock()
	defer j.mu.Unlock()

	entry := j.findLatest(path)
	if entry == nil {
		return ""
	}
	return unifiedDiff(path, entry.Before, entry.After)
}

func (j *Journal) findLatest(path string) *Entry {
	for i := len(j.entries) - 1; i >= 0; i-- {
		if j.entries[i].Path == path {
			return &j.entries[i]
		}
	}
	return nil
}

// unifiedDiff produces a simple unified diff between two strings.
func unifiedDiff(path, before, after string) string {
	oldLines := splitLines(before)
	newLines := splitLines(after)

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("--- a/%s\n", path))
	buf.WriteString(fmt.Sprintf("+++ b/%s\n", path))

	writeHunks(&buf, oldLines, newLines)
	return buf.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// writeHunks writes diff hunks comparing old and new lines.
func writeHunks(buf *strings.Builder, old, new []string) {
	maxLen := len(old)
	if len(new) > maxLen {
		maxLen = len(new)
	}
	for i := 0; i < maxLen; i++ {
		oldLine := lineAt(old, i)
		newLine := lineAt(new, i)
		if oldLine == newLine {
			buf.WriteString(" " + oldLine + "\n")
			continue
		}
		if i < len(old) {
			buf.WriteString("-" + oldLine + "\n")
		}
		if i < len(new) {
			buf.WriteString("+" + newLine + "\n")
		}
	}
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}
