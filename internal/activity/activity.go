package activity

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// Entry is a single line from an activity JSONL log.
type Entry struct {
	Timestamp time.Time `json:"ts"`
	State     string    `json:"activity,omitempty"` // "active", "waiting_input", "blocked", etc.
	Source    string    `json:"type,omitempty"`      // "agent_active", "permission_request", "error", etc.
}

// ActivityState mirrors workspace.ActivityState for use without circular imports.
type ActivityState = string

const (
	StateActive    ActivityState = "active"
	StateReady     ActivityState = "ready"
	StateIdle      ActivityState = "idle"
	StateWaitInput ActivityState = "waiting_input"
	StateBlocked   ActivityState = "blocked"
	StateExited    ActivityState = "exited"
)

// Detection holds the result of checking an agent's activity.
type Detection struct {
	State     ActivityState
	Timestamp time.Time
	Source    string // "process_dead", "jsonl_actionable", "jsonl_age", "native_signal"
}

// ReadLastActivityEntry reads the last line of a JSONL activity log file
// and parses it into an Entry. Returns nil, nil if the file does not exist
// or is empty.
func ReadLastActivityEntry(path string) (*Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var lastLine string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lastLine = line
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if lastLine == "" {
		return nil, nil
	}

	var entry Entry
	if err := json.Unmarshal([]byte(lastLine), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// CheckActivityLogState inspects a JSONL entry for actionable states
// (waiting_input, blocked) and returns a Detection if found. Returns nil
// if the entry does not indicate an actionable state.
func CheckActivityLogState(entry *Entry) *Detection {
	if entry == nil {
		return nil
	}
	switch {
	case entry.State == "waiting_input" ||
		entry.Source == "permission_request":
		return &Detection{
			State:     StateWaitInput,
			Timestamp: entry.Timestamp,
			Source:    "jsonl_actionable",
		}
	case entry.State == "blocked" ||
		entry.Source == "error":
		return &Detection{
			State:     StateBlocked,
			Timestamp: entry.Timestamp,
			Source:    "jsonl_actionable",
		}
	}
	return nil
}

// GetActivityFallbackState determines active vs idle from the JSONL entry's
// timestamp age. activeWindowMs is how long (ms) before an entry is
// considered stale; thresholdMs is the cutoff for idle.
func GetActivityFallbackState(
	entry *Entry,
	activeWindowMs, thresholdMs int64,
) *Detection {
	if entry == nil {
		return nil
	}
	age := time.Since(entry.Timestamp).Milliseconds()
	if age <= activeWindowMs {
		return &Detection{
			State:     StateActive,
			Timestamp: entry.Timestamp,
			Source:    "jsonl_age",
		}
	}
	if age <= thresholdMs {
		return &Detection{
			State:     StateReady,
			Timestamp: entry.Timestamp,
			Source:    "jsonl_age",
		}
	}
	return &Detection{
		State:     StateIdle,
		Timestamp: entry.Timestamp,
		Source:    "jsonl_age",
	}
}
