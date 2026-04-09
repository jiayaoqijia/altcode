package tui

import "strings"

const maxHistory = 100

// inputHistory stores previous prompts for up/down recall.
type inputHistory struct {
	entries []string // oldest first
	cursor  int      // -1 = not browsing, 0..len-1 = position from end
	draft   string   // text in input before user started browsing
}

func newInputHistory() *inputHistory {
	return &inputHistory{cursor: -1}
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
