package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jiayaoqijia/altcode/internal/store"
)

// TestAddMessage_BumpsSessionUpdatedAt guards the Codex round-Q
// finding: AddMessage inserted into `message` but never bumped
// `session.updated_at`, so LatestSession() and any UI ordered by
// recency showed stale sessions as "recent" and active sessions
// with N new messages as "old".
func TestAddMessage_BumpsSessionUpdatedAt(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	sess, err := db.CreateSession("proj-1", "test session", "openai/gpt-5.4")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	initialUpdated := sess.UpdatedAt

	// Wait ~10ms so UnixMilli differs from initialUpdated.
	time.Sleep(15 * time.Millisecond)
	if _, err := db.AddMessage(sess.ID, "user",
		[]byte("hello"), "openai/gpt-5.4", 0, 0); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	// Re-read the session; updated_at must have moved forward.
	reread, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !reread.UpdatedAt.After(initialUpdated) {
		t.Errorf("session.updated_at did not advance after AddMessage: "+
			"was %v, now %v", initialUpdated, reread.UpdatedAt)
	}

	// LatestSession should return this session (only one in the db).
	latest, err := db.LatestSession("proj-1")
	if err != nil {
		t.Fatalf("LatestSession: %v", err)
	}
	if latest == nil || latest.ID != sess.ID {
		t.Errorf("LatestSession = %+v, want %s", latest, sess.ID)
	}
}
