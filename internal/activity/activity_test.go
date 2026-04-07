package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writeJSONL(t *testing.T, path string, entries ...Entry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, e := range entries {
		data, _ := json.Marshal(e)
		f.Write(data)
		f.WriteString("\n")
	}
}

// Test 1: Returns exited when process not running.
// (Activity cascade: process liveness is checked externally; when the entry
// source says "process_dead", the caller maps that to exited. Here we test
// that CheckActivityLogState returns nil for a non-actionable entry, which
// signals the caller to fall through to the process-liveness check.)
func TestActivityState_Exited(t *testing.T) {
	entry := &Entry{
		Timestamp: time.Now(),
		Source:    "process_dead",
	}
	det := CheckActivityLogState(entry)
	if det != nil {
		t.Errorf("expected nil for process_dead source, got %+v", det)
	}
}

// Test 2: Returns waiting_input from JSONL.
func TestActivityState_WaitingInput(t *testing.T) {
	p := filepath.Join(t.TempDir(), "activity.jsonl")
	ts := time.Now().Truncate(time.Second)
	writeJSONL(t, p, Entry{
		Timestamp: ts,
		State:     "waiting_input",
		Source:    "permission_request",
	})

	entry, err := ReadLastActivityEntry(p)
	if err != nil {
		t.Fatalf("ReadLastActivityEntry: %v", err)
	}
	det := CheckActivityLogState(entry)
	if det == nil {
		t.Fatal("expected detection, got nil")
	}
	if det.State != StateWaitInput {
		t.Errorf("State = %q, want %q", det.State, StateWaitInput)
	}
	if det.Source != "jsonl_actionable" {
		t.Errorf("Source = %q, want %q", det.Source, "jsonl_actionable")
	}
}

// Test 3: Returns blocked from JSONL.
func TestActivityState_Blocked(t *testing.T) {
	p := filepath.Join(t.TempDir(), "activity.jsonl")
	ts := time.Now().Truncate(time.Second)
	writeJSONL(t, p, Entry{
		Timestamp: ts,
		State:     "blocked",
		Source:    "error",
	})

	entry, err := ReadLastActivityEntry(p)
	if err != nil {
		t.Fatalf("ReadLastActivityEntry: %v", err)
	}
	det := CheckActivityLogState(entry)
	if det == nil {
		t.Fatal("expected detection, got nil")
	}
	if det.State != StateBlocked {
		t.Errorf("State = %q, want %q", det.State, StateBlocked)
	}
}

// Test 4: Returns active from fresh entry.
func TestActivityState_Active(t *testing.T) {
	entry := &Entry{
		Timestamp: time.Now(),
		State:     "active",
		Source:    "agent_active",
	}
	// Not an actionable state, so CheckActivityLogState returns nil.
	det := CheckActivityLogState(entry)
	if det != nil {
		t.Fatalf("expected nil, got %+v", det)
	}
	// Fallback: fresh entry within window -> active.
	fb := GetActivityFallbackState(entry, 30_000, 300_000)
	if fb == nil {
		t.Fatal("expected fallback detection, got nil")
	}
	if fb.State != StateActive {
		t.Errorf("State = %q, want %q", fb.State, StateActive)
	}
}

// Test 5: Returns idle from old entry.
func TestActivityState_Idle(t *testing.T) {
	entry := &Entry{
		Timestamp: time.Now().Add(-10 * time.Minute),
		State:     "active",
		Source:    "agent_active",
	}
	fb := GetActivityFallbackState(entry, 30_000, 300_000)
	if fb == nil {
		t.Fatal("expected fallback detection, got nil")
	}
	if fb.State != StateIdle {
		t.Errorf("State = %q, want %q", fb.State, StateIdle)
	}
}

// Test 6: Returns nil when no data.
func TestActivityState_NoData(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nonexistent.jsonl")
	entry, err := ReadLastActivityEntry(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil entry, got %+v", entry)
	}

	det := CheckActivityLogState(nil)
	if det != nil {
		t.Errorf("expected nil detection, got %+v", det)
	}

	fb := GetActivityFallbackState(nil, 30_000, 300_000)
	if fb != nil {
		t.Errorf("expected nil fallback, got %+v", fb)
	}
}

// Test 7: Concurrent read/write safety.
func TestActivityState_ConcurrentReadWrite(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "activity.jsonl")

	var wg sync.WaitGroup
	n := 50

	// Writers.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			f, err := os.OpenFile(
				p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644,
			)
			if err != nil {
				t.Errorf("open: %v", err)
				return
			}
			defer f.Close()
			e := Entry{
				Timestamp: time.Now(),
				State:     "active",
				Source:    "agent_active",
			}
			data, _ := json.Marshal(e)
			data = append(data, '\n')
			f.Write(data)
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// ReadLastActivityEntry must not panic.
			_, _ = ReadLastActivityEntry(p)
		}()
	}

	wg.Wait()

	// Final read should succeed.
	entry, err := ReadLastActivityEntry(p)
	if err != nil {
		t.Fatalf("final read: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry after writes, got nil")
	}
}
