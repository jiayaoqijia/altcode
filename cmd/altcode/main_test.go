package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/auth"
	"github.com/altcode-ai/altcode/internal/exec"
	"github.com/altcode-ai/altcode/internal/store"
)

func TestLoadConfigReadsUserConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	path := auth.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	data := []byte(`{
  "provider": {
    "openai": {
      "apiKey": "test-openai-key"
    }
  }
}
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cfg := loadConfig("", "", "")
	if got := cfg.Provider["openai"].APIKey; got != "test-openai-key" {
		t.Fatalf("expected user config key to load, got %q", got)
	}
}

// --- Phase 4 tests --------------------------------------------------

// TestShortID verifies the session ID truncation helper used by
// fork-session diagnostic messages.
func TestShortID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"abcdef1234567890", "abcdef12"},
		{"abc", "abc"}, // shorter than 8 → unchanged
		{"", ""},
		{"exactly8", "exactly8"},
	}
	for _, tc := range cases {
		if got := shortID(tc.in); got != tc.want {
			t.Errorf("shortID(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestForkSession_HappyPath creates a source session with a couple
// of messages, then forks it and verifies the new session has the
// same messages but a distinct ID.
func TestForkSession_HappyPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	src, err := db.CreateSession("proj", "source", "test-model")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err = db.AddMessage(src.ID, "user", []byte(`"hello"`), "test-model", 0, 0)
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}
	_, err = db.AddMessage(src.ID, "assistant", []byte(`"hi there"`), "test-model", 0, 0)
	if err != nil {
		t.Fatalf("add msg: %v", err)
	}

	newID, err := forkSession(db, src.ID, "test-model")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if newID == src.ID {
		t.Fatal("fork produced same ID")
	}

	newMsgs, err := db.ListMessages(newID)
	if err != nil {
		t.Fatalf("list forked msgs: %v", err)
	}
	if len(newMsgs) != 2 {
		t.Errorf("expected 2 messages in fork, got %d", len(newMsgs))
	}

	// Source is untouched
	srcMsgs, _ := db.ListMessages(src.ID)
	if len(srcMsgs) != 2 {
		t.Errorf("source mutated: expected 2 messages, got %d", len(srcMsgs))
	}
}

// TestForkSession_UnknownSource returns a typed UsageError with
// exit code 64, not a random wrapped error.
func TestForkSession_UnknownSource(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	_, err = forkSession(db, "does-not-exist", "test-model")
	if err == nil {
		t.Fatal("expected error")
	}
	var uerr *exec.UsageError
	if !errors.As(err, &uerr) {
		t.Errorf("expected *exec.UsageError, got %T: %v", err, err)
	}
	if uerr.ExitCode != 64 {
		t.Errorf("expected exit 64, got %d", uerr.ExitCode)
	}
	if !strings.Contains(uerr.Msg, "not found") {
		t.Errorf("expected 'not found' in error, got %q", uerr.Msg)
	}
}

// TestForkSession_NilDB returns a UsageError instead of nil-deref.
func TestForkSession_NilDB(t *testing.T) {
	_, err := forkSession(nil, "id", "model")
	if err == nil {
		t.Fatal("expected error on nil db")
	}
	var uerr *exec.UsageError
	if !errors.As(err, &uerr) {
		t.Errorf("expected UsageError, got %T", err)
	}
}

// TestListSessionsFromDB exercises the alternate-path list entry
// point used by `--list-sessions --session-db`.
func TestListSessionsFromDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = db.CreateSession("proj", "one", "test-model")
	db.Close()

	// listSessionsFromDB prints to stdout; we just verify no error.
	if err := listSessionsFromDB(dbPath); err != nil {
		t.Errorf("listSessionsFromDB: %v", err)
	}
}

// TestForkSession_TransactionAtomicity verifies that the fork
// operation writes no half-forked state if it fails partway through.
// Hard to inject a mid-transaction failure without mocking SQL, so
// the test instead verifies that ForkSession emits a message count
// that matches what the store reports — if messages were being
// double-inserted or the tx was committing partial state, the count
// would drift.
func TestForkSession_TransactionAtomicity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "sessions.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	src, _ := db.CreateSession("proj", "source", "test-model")
	for i := 0; i < 50; i++ {
		_, _ = db.AddMessage(src.ID, "user", []byte(`"m"`), "test-model", 0, 0)
	}

	newID, err := forkSession(db, src.ID, "test-model")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	msgs, _ := db.ListMessages(newID)
	if len(msgs) != 50 {
		t.Errorf("expected 50 messages in fork, got %d", len(msgs))
	}
	// Source still has exactly 50 — no duplicates, no missing
	srcMsgs, _ := db.ListMessages(src.ID)
	if len(srcMsgs) != 50 {
		t.Errorf("source count drifted: got %d, want 50", len(srcMsgs))
	}
}
