package store

import (
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSessionCRUD(t *testing.T) {
	db := openTestDB(t)

	// Create
	s, err := db.CreateSession("proj-1", "My Session", "claude-3")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if s.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if s.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want %q", s.ProjectID, "proj-1")
	}
	if s.Title != "My Session" {
		t.Errorf("Title = %q, want %q", s.Title, "My Session")
	}
	if s.Model != "claude-3" {
		t.Errorf("Model = %q, want %q", s.Model, "claude-3")
	}
	if s.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	// Get
	got, err := db.GetSession(s.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != s.ID {
		t.Errorf("ID = %q, want %q", got.ID, s.ID)
	}
	if got.Title != s.Title {
		t.Errorf("Title = %q, want %q", got.Title, s.Title)
	}

	// List
	list, err := db.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListSessions len = %d, want 1", len(list))
	}

	// updateSessionTitle
	if err := db.updateSessionTitle(s.ID, "Renamed"); err != nil {
		t.Fatalf("updateSessionTitle: %v", err)
	}
	updated, err := db.GetSession(s.ID)
	if err != nil {
		t.Fatalf("GetSession after update: %v", err)
	}
	if updated.Title != "Renamed" {
		t.Errorf("updated Title = %q, want %q", updated.Title, "Renamed")
	}

	// GetSession with unknown ID should error
	_, err = db.GetSession("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session, got nil")
	}
}

func TestConcurrentSessions(t *testing.T) {
	db := openTestDB(t)

	// Create sessions for multiple projects
	s1, _ := db.CreateSession("proj-a", "Session A1", "claude-3")
	s2, _ := db.CreateSession("proj-a", "Session A2", "claude-3")
	s3, _ := db.CreateSession("proj-b", "Session B1", "claude-3")

	// All should be listed
	list, _ := db.ListSessions()
	if len(list) != 3 {
		t.Fatalf("Expected 3 sessions, got %d", len(list))
	}

	// IDs should all be unique
	ids := map[string]bool{}
	for _, s := range []*Session{s1, s2, s3} {
		if ids[s.ID] {
			t.Errorf("Duplicate session ID: %s", s.ID)
		}
		ids[s.ID] = true
	}
}

func TestUpdateSessionTitle_NonexistentReturnsError(t *testing.T) {
	db := openTestDB(t)
	err := db.updateSessionTitle("nonexistent-id", "New Title")
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func TestMessageOrdering(t *testing.T) {
	db := openTestDB(t)
	sess, _ := db.CreateSession("proj", "ordering", "model")

	db.AddMessage(sess.ID, "user", []byte("first"), "m", 0, 0)
	db.AddMessage(sess.ID, "assistant", []byte("second"), "m", 0, 0)
	db.AddMessage(sess.ID, "user", []byte("third"), "m", 0, 0)

	msgs, _ := db.ListMessages(sess.ID)
	if len(msgs) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(msgs))
	}
	if string(msgs[0].Content) != "first" {
		t.Errorf("First message: %q", string(msgs[0].Content))
	}
	if string(msgs[1].Content) != "second" {
		t.Errorf("Second message: %q", string(msgs[1].Content))
	}
	if string(msgs[2].Content) != "third" {
		t.Errorf("Third message: %q", string(msgs[2].Content))
	}
}

func TestNewSession(t *testing.T) {
	db := openTestDB(t)
	s, err := db.CreateSession("proj", "test", "model")
	if err != nil {
		t.Fatal(err)
	}
	if s.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
	if s.Summary != "" {
		t.Error("Summary should default to empty")
	}
}

func TestMessageCRUD(t *testing.T) {
	db := openTestDB(t)

	sess, err := db.CreateSession("proj-2", "Chat", "claude-3")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Add messages
	m1, err := db.AddMessage(sess.ID, "user", []byte("Hello"), "claude-3", 5, 0)
	if err != nil {
		t.Fatalf("AddMessage user: %v", err)
	}
	if m1.ID == "" {
		t.Fatal("expected non-empty message ID")
	}

	m2, err := db.AddMessage(sess.ID, "assistant", []byte("Hi there!"), "claude-3", 0, 10)
	if err != nil {
		t.Fatalf("AddMessage assistant: %v", err)
	}

	// List
	msgs, err := db.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("ListMessages len = %d, want 2", len(msgs))
	}

	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want user", msgs[0].Role)
	}
	if string(msgs[0].Content) != "Hello" {
		t.Errorf("msgs[0].Content = %q, want Hello", msgs[0].Content)
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want assistant", msgs[1].Role)
	}

	// Verify IDs differ
	if m1.ID == m2.ID {
		t.Error("message IDs should be unique")
	}

	// ListMessages for unknown session returns empty
	empty, err := db.ListMessages("no-such-session")
	if err != nil {
		t.Fatalf("ListMessages unknown session: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 messages for unknown session, got %d", len(empty))
	}
}
