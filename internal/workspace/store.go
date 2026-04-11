package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Store provides flat-file persistence for workspace sessions.
// All writes are atomic (write-tmp + rename). All reads are lock-free.
type Store struct {
	root string // .altcode/workspace/ directory path
}

// NewStore creates a Store rooted at the given directory.
func NewStore(root string) *Store {
	return &Store{root: root}
}

// SaveSession writes sess to {root}/{id}/session.json atomically.
// It writes to a temporary file then renames, preventing corrupt reads
// if the process is killed mid-write.
func (s *Store) SaveSession(sess *WorkspaceSession) error {
	sess.mu.Lock()
	sess.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(sess, "", "  ")
	sess.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	dir := filepath.Join(s.root, sess.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	p := filepath.Join(dir, "session.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// LoadSession reads {root}/{id}/session.json and returns the session.
// Validates that the ID does not contain path separators to prevent
// traversal. Previous validation used ContainsAny("/\\..") which is
// a CHARACTER set check, so any single dot rejected the id — that
// would silently break any future ID scheme that includes a dot
// (timestamp-prefixed, semver-suffixed, etc.). Now checks for the
// real path separators and the literal ".." substring.
func (s *Store) LoadSession(id string) (*WorkspaceSession, error) {
	if id == "" || id != filepath.Base(id) ||
		strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return nil, fmt.Errorf("invalid session ID: %q", id)
	}
	p := filepath.Join(s.root, id, "session.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read session %s: %w", id, err)
	}
	var sess WorkspaceSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal session %s: %w", id, err)
	}
	return &sess, nil
}

// ListSessions returns workspace IDs found under root.
// Each ID corresponds to a subdirectory containing session.json.
func (s *Store) ListSessions() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sp := filepath.Join(s.root, e.Name(), "session.json")
		if _, err := os.Stat(sp); err == nil {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// AppendActivity appends a JSON-encoded entry as a single line to
// {root}/{id}/activity.jsonl. The write is flock-guarded for safety
// under concurrent appenders.
func (s *Store) AppendActivity(id string, entry any) error {
	dir := filepath.Join(s.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	p := filepath.Join(dir, "activity.jsonl")
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal activity: %w", err)
	}
	line := string(data) + "\n"

	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open activity: %w", err)
	}
	// Track lock ownership explicitly so we never call LOCK_UN on an
	// fd we don't actually hold. Previously the deferred cleanup
	// blindly called LOCK_UN even when Flock had failed, which is
	// the classic 'unlock without ownership' shape.
	locked := false
	defer func() {
		if locked {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
		}
		f.Close()
	}()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	locked = true

	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("write activity: %w", err)
	}
	// Durability: fsync the activity log so a crash between write
	// and the next checkpoint doesn't lose the most recent record.
	// Workflow recovery and audit trails depend on this being
	// crash-consistent, not just delivered to the page cache.
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync activity: %w", err)
	}
	return nil
}

// SendMessage sends a message to a running agent in a workspace session.
// For claude backend: kills the running process and relaunches with --resume + message.
// For codex/opencode/aider: appends to context.md and enqueues in agents/{role}.json.
// Full implementation requires agent backends (Phase 3). This is the method signature
// needed by the lifecycle manager's DispatchCIFix and review feedback routing.
func (s *Store) SendMessage(ctx context.Context, sessionID, role, message string) error {
	sess, err := s.LoadSession(sessionID)
	if err != nil {
		return fmt.Errorf("SendMessage: load session: %w", err)
	}
	_, ok := sess.Agents[role]
	if !ok {
		return fmt.Errorf("SendMessage: no agent with role %q in session %s", role, sessionID)
	}
	// Append to context.md so it's visible on next spawn
	ctxPath := filepath.Join(s.root, sessionID, "context.md")
	f, err := os.OpenFile(ctxPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("SendMessage: open context.md: %w", err)
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n## Operator Message to %s\n\n%s\n", role, message)
	if err != nil {
		return fmt.Errorf("SendMessage: write context.md: %w", err)
	}
	// Log to activity JSONL
	return s.AppendActivity(sessionID, map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"type":    "operator_inject",
		"role":    role,
		"message": message,
	})
}

// readActivityLines reads all lines from {root}/{id}/activity.jsonl.
// Returns nil, nil if the file does not exist.
func (s *Store) readActivityLines(id string) ([]string, error) {
	p := filepath.Join(s.root, id, "activity.jsonl")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}
