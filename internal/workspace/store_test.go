package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSaveLoadSession(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	now := time.Now().Truncate(time.Second)
	sess := &WorkspaceSession{
		ID:         "test-001",
		Task:       "add auth",
		Status:     WSSWorking,
		GitRoot:    "/repo",
		BaseBranch: "main",
		CreatedAt:  now,
		Agents: map[string]*AgentRecord{
			"architect": {
				Role:    "architect",
				Backend: "claude",
				Branch:  "altcode/architect/auth",
			},
		},
		MaxCIRetries: 3,
	}

	if err := s.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	loaded, err := s.LoadSession("test-001")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	if loaded.ID != sess.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, sess.ID)
	}
	if loaded.Task != sess.Task {
		t.Errorf("Task = %q, want %q", loaded.Task, sess.Task)
	}
	if loaded.Status != sess.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, sess.Status)
	}
	if loaded.MaxCIRetries != 3 {
		t.Errorf("MaxCIRetries = %d, want 3", loaded.MaxCIRetries)
	}
	rec := loaded.Agents["architect"]
	if rec == nil {
		t.Fatal("agent record 'architect' missing")
	}
	if rec.Backend != "claude" {
		t.Errorf("Backend = %q, want %q", rec.Backend, "claude")
	}
}

func TestListSessions(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	// Empty root returns nil.
	ids, err := s.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions empty: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(ids))
	}

	// Create two sessions.
	for _, id := range []string{"ws-aaa", "ws-bbb"} {
		sess := &WorkspaceSession{
			ID:     id,
			Task:   "test",
			Status: WSSWorking,
			Agents: map[string]*AgentRecord{},
		}
		if err := s.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession %s: %v", id, err)
		}
	}

	// Create a stray directory without session.json.
	os.MkdirAll(filepath.Join(root, "stray"), 0o755)

	ids, err = s.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 sessions, got %d: %v", len(ids), ids)
	}
}

func TestAppendActivity(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	id := "ws-activity"
	os.MkdirAll(filepath.Join(root, id), 0o755)

	type entry struct {
		Type string `json:"type"`
		N    int    `json:"n"`
	}

	// Concurrent appenders.
	var wg sync.WaitGroup
	n := 20
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			e := entry{Type: "test", N: idx}
			if err := s.AppendActivity(id, e); err != nil {
				t.Errorf("AppendActivity(%d): %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	lines, err := s.readActivityLines(id)
	if err != nil {
		t.Fatalf("readActivityLines: %v", err)
	}
	if len(lines) != n {
		t.Fatalf("expected %d lines, got %d", n, len(lines))
	}

	// Each line must be valid JSON.
	for i, line := range lines {
		var e entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Errorf("line %d invalid JSON: %v", i, err)
		}
	}
}

func TestSaveSession_Atomic(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)

	sess := &WorkspaceSession{
		ID:     "ws-atomic",
		Task:   "atomic test",
		Status: WSSSpawning,
		Agents: map[string]*AgentRecord{},
	}
	if err := s.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Verify session.json exists and tmp does NOT.
	dir := filepath.Join(root, "ws-atomic")
	if _, err := os.Stat(filepath.Join(dir, "session.json")); err != nil {
		t.Errorf("session.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json.tmp")); !os.IsNotExist(err) {
		t.Error("session.json.tmp should not exist after save")
	}

	// Overwrite and verify round-trip.
	sess.Status = WSSWorking
	if err := s.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession overwrite: %v", err)
	}
	loaded, err := s.LoadSession("ws-atomic")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Status != WSSWorking {
		t.Errorf("Status = %q, want %q", loaded.Status, WSSWorking)
	}
}
