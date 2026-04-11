package workspace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
)

// EventType classifies workspace events in the append-only log.
type EventType string

const (
	EventAgentSpawned    EventType = "agent_spawned"
	EventAgentOutput     EventType = "agent_output"
	EventAgentExited     EventType = "agent_exited"
	EventToolCall        EventType = "tool_call"
	EventTaskClaimed     EventType = "task_claimed"
	EventTaskCompleted   EventType = "task_completed"
	EventCIStatus        EventType = "ci_status"
	EventPRCreated       EventType = "pr_created"
	EventOperatorMessage EventType = "operator_message"
	EventError           EventType = "error"
)

// Event is a single entry in the durable session event log.
type Event struct {
	ID        string    `json:"id"`
	Type      EventType `json:"type"`
	Role      string    `json:"role,omitempty"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// EventLog is an append-only JSONL log for workspace events.
// Path: .altcode/workspace/{id}/events.jsonl
type EventLog struct {
	path string
}

// NewEventLog creates an EventLog writing to the given path.
func NewEventLog(path string) *EventLog {
	return &EventLog{path: path}
}

// Emit appends a single event to the JSONL log. Writes are
// flock-guarded for safety under concurrent appenders.
func (l *EventLog) Emit(ev Event) error {
	if ev.ID == "" {
		ev.ID = ulid.Make().String()
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	line := string(data) + "\n"

	f, err := os.OpenFile(
		l.path,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	if err := syscall.Flock(
		int(f.Fd()), syscall.LOCK_EX,
	); err != nil {
		f.Close()
		return fmt.Errorf("flock event log: %w", err)
	}
	if _, err := f.WriteString(line); err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
		f.Close()
		return fmt.Errorf("write event: %w", err)
	}
	// Sync + Close errors must be checked on the write path. The
	// previous defer-and-discard pattern returned nil even when the
	// fsync failed (NFS, FUSE, quota exceeded), so callers thought
	// the event was durable when it had silently been lost. Sync
	// before unlocking so concurrent readers see consistent state.
	if err := f.Sync(); err != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
		f.Close()
		return fmt.Errorf("sync event log: %w", err)
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	if err := f.Close(); err != nil {
		return fmt.Errorf("close event log: %w", err)
	}
	return nil
}

// GetEvents returns events with timestamps after since.
func (l *EventLog) GetEvents(
	since time.Time,
) ([]Event, error) {
	all, err := l.GetAll()
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, ev := range all {
		if ev.Timestamp.After(since) {
			out = append(out, ev)
		}
	}
	return out, nil
}

// GetAll reads and returns every event in the log.
func (l *EventLog) GetAll() ([]Event, error) {
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip corrupt lines
		}
		events = append(events, ev)
	}
	return events, sc.Err()
}

// Tail returns the last n events from the log.
func (l *EventLog) Tail(n int) ([]Event, error) {
	all, err := l.GetAll()
	if err != nil {
		return nil, err
	}
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}
