package tui

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const maxHistory = 100

// inputHistory stores previous prompts for up/down recall, optionally
// persisted to disk so Up arrow recall survives restarts.
type inputHistory struct {
	entries []string // oldest first
	cursor  int      // -1 = not browsing, 0..len-1 = position from end
	draft   string   // text in input before user started browsing
	path    string   // file to persist to ("" = ephemeral)
}

func newInputHistory() *inputHistory {
	return &inputHistory{cursor: -1}
}

// newPersistentInputHistory loads an existing history file (if any)
// and remembers the path so future Add calls persist back to it.
// Errors are non-fatal — a missing or unreadable file just yields
// an empty in-memory history.
func newPersistentInputHistory(path string) *inputHistory {
	h := &inputHistory{cursor: -1, path: path}
	h.load()
	return h
}

// DefaultHistoryPath returns the conventional location for the
// persisted input history. Honors $XDG_STATE_HOME, then $HOME.
func DefaultHistoryPath() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "altcode", "input_history")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "altcode", "input_history")
}

// load reads existing history from disk into entries.
func (h *inputHistory) load() {
	if h.path == "" {
		return
	}
	f, err := os.Open(h.path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		h.entries = append(h.entries, line)
	}
	if len(h.entries) > maxHistory {
		h.entries = h.entries[len(h.entries)-maxHistory:]
	}
}

// save writes the current entries to the persistence file.
// Atomic temp+rename so a crash mid-write never truncates the file.
// Errors are silent — losing one history append is preferable to
// crashing the TUI.
func (h *inputHistory) save() {
	if h.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".history-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	w := bufio.NewWriter(tmp)
	for _, e := range h.entries {
		// Skip entries that contain newlines so the line-oriented
		// load() roundtrip is lossless.
		if strings.ContainsRune(e, '\n') {
			continue
		}
		w.WriteString(e)
		w.WriteByte('\n')
	}
	w.Flush()
	tmp.Close()
	_ = os.Chmod(tmpName, 0o600)
	if err := os.Rename(tmpName, h.path); err != nil {
		os.Remove(tmpName)
	}
}

// Add saves a prompt to history (skips empty/duplicate).
func (h *inputHistory) Add(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// Deduplicate: don't add if same as last entry
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == text {
		return
	}
	h.entries = append(h.entries, text)
	if len(h.entries) > maxHistory {
		h.entries = h.entries[len(h.entries)-maxHistory:]
	}
	h.cursor = -1
	h.save()
}

// Up moves to the previous (older) entry. Returns the text to show.
func (h *inputHistory) Up(currentInput string) (string, bool) {
	if len(h.entries) == 0 {
		return "", false
	}
	if h.cursor == -1 {
		// Start browsing — save current input as draft
		h.draft = currentInput
		h.cursor = len(h.entries) - 1
	} else if h.cursor > 0 {
		h.cursor--
	} else {
		return h.entries[0], false // already at oldest
	}
	return h.entries[h.cursor], true
}

// Down moves to the next (newer) entry. Returns the text to show.
func (h *inputHistory) Down() (string, bool) {
	if h.cursor == -1 {
		return "", false
	}
	if h.cursor < len(h.entries)-1 {
		h.cursor++
		return h.entries[h.cursor], true
	}
	// Past newest → restore draft
	h.cursor = -1
	return h.draft, true
}

// Search finds entries containing query, returns matches (newest first).
func (h *inputHistory) Search(query string) []string {
	if query == "" {
		return nil
	}
	query = strings.ToLower(query)
	var matches []string
	for i := len(h.entries) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(h.entries[i]), query) {
			matches = append(matches, h.entries[i])
			if len(matches) >= 10 {
				break
			}
		}
	}
	return matches
}

// Reset stops browsing history.
func (h *inputHistory) Reset() {
	h.cursor = -1
}

// Len returns the number of history entries.
func (h *inputHistory) Len() int {
	return len(h.entries)
}
