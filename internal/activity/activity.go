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
//
// Reads from the END of the file (a tail-window) instead of scanning
// from the start. The previous implementation scanned every line on
// every poll tick — for a long-running session with a multi-MB JSONL
// log, the lifecycle's 10s poll loop produced O(n²) total I/O over
// the session lifetime. Tail-reading is constant work regardless of
// how big the file gets.
func ReadLastActivityEntry(path string) (*Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	lastLine, err := readLastNonEmptyLine(f)
	if err != nil {
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

// readLastNonEmptyLine reads the file from the end backwards in
// 8KB chunks, returning the last non-empty line. Falls back to a
// full scan only if the file fits in a single chunk.
func readLastNonEmptyLine(f *os.File) (string, error) {
	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := stat.Size()
	if size == 0 {
		return "", nil
	}

	const chunkSize = int64(8192)
	var (
		buf       []byte
		readPos   = size
		hadAnyNL  = false
	)
	for readPos > 0 {
		readBytes := chunkSize
		if readPos < chunkSize {
			readBytes = readPos
		}
		readPos -= readBytes
		chunk := make([]byte, readBytes)
		if _, err := f.ReadAt(chunk, readPos); err != nil {
			return "", err
		}
		buf = append(chunk, buf...)
		// Stop once we have at least one complete line. The last
		// non-empty line may end with a newline that comes BEFORE
		// the EOF; we need at least two newlines (one before the
		// last line and one after) OR the start of the file.
		nlCount := 0
		for _, b := range buf {
			if b == '\n' {
				nlCount++
				hadAnyNL = true
			}
		}
		if nlCount >= 2 || readPos == 0 {
			break
		}
		// Cap memory: don't pull the whole file into RAM if it
		// has very long lines and no newlines for many KB.
		if int64(len(buf)) > 1024*1024 {
			break
		}
	}

	// Walk lines from the end picking the last non-empty one.
	lines := splitLinesPreserveTail(buf, hadAnyNL)
	for i := len(lines) - 1; i >= 0; i-- {
		if line := lines[i]; line != "" {
			return line, nil
		}
	}
	// Fallback: file is small or all whitespace — degrade to a
	// linear scan so the empty-file path remains correct.
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}
	var lastLine string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		l := scanner.Text()
		if l != "" {
			lastLine = l
		}
	}
	return lastLine, scanner.Err()
}

func splitLinesPreserveTail(buf []byte, hadAnyNL bool) []string {
	if !hadAnyNL {
		return []string{string(buf)}
	}
	var lines []string
	start := 0
	for i, b := range buf {
		if b == '\n' {
			lines = append(lines, string(buf[start:i]))
			start = i + 1
		}
	}
	if start < len(buf) {
		lines = append(lines, string(buf[start:]))
	}
	return lines
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
