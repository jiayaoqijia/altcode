package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session represents a conversation session.
type Session struct {
	ID        string
	ProjectID string
	Title     string
	Model     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Summary   string
}

// CreateSession inserts a new session and returns it with a generated ID.
func (db *DB) CreateSession(projectID, title, model string) (*Session, error) {
	now := time.Now()
	s := &Session{
		ID:        newID(),
		ProjectID: projectID,
		Title:     title,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := db.sql.Exec(
		`INSERT INTO session (id, project_id, title, model, created_at, updated_at, summary)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.ProjectID, s.Title, s.Model,
		s.CreatedAt.UnixMilli(), s.UpdatedAt.UnixMilli(), s.Summary,
	)
	if err != nil {
		return nil, fmt.Errorf("store: create session: %w", err)
	}
	return s, nil
}

// ErrSessionNotFound is returned by GetSession/ForkSession when the
// requested session ID isn't in the store. Callers should prefer
// errors.Is(err, ErrSessionNotFound) over substring matching the
// error message.
var ErrSessionNotFound = errors.New("store: session not found")

// ForkSession copies all messages from sourceID into a newly-created
// session and returns the new session. The entire copy happens in a
// single transaction so a crash or cancel can't leave a half-forked
// session behind. Forked messages preserve their original CreatedAt
// timestamps so audit/ordering semantics survive the fork. The
// source session is not mutated.
//
// Returns an error wrapping ErrSessionNotFound if sourceID doesn't
// exist in the store.
func (db *DB) ForkSession(sourceID, titleOverride, modelFallback string) (*Session, int, error) {
	src, err := db.GetSession(sourceID)
	if err != nil {
		return nil, 0, err
	}
	msgs, err := db.ListMessages(sourceID)
	if err != nil {
		return nil, 0, fmt.Errorf("store: fork list messages: %w", err)
	}

	model := src.Model
	if model == "" {
		model = modelFallback
	}
	title := titleOverride
	if title == "" {
		title = "fork of " + shortSessionID(sourceID)
	}

	tx, err := db.sql.Begin()
	if err != nil {
		return nil, 0, fmt.Errorf("store: fork begin tx: %w", err)
	}
	// Rollback on any error path; Commit is a no-op if already committed.
	defer func() { _ = tx.Rollback() }()

	now := time.Now()
	newSess := &Session{
		ID:        newID(),
		ProjectID: src.ProjectID,
		Title:     title,
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := tx.Exec(
		`INSERT INTO session (id, project_id, title, model, created_at, updated_at, summary)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		newSess.ID, newSess.ProjectID, newSess.Title, newSess.Model,
		newSess.CreatedAt.UnixMilli(), newSess.UpdatedAt.UnixMilli(), newSess.Summary,
	); err != nil {
		return nil, 0, fmt.Errorf("store: fork create session: %w", err)
	}

	// Bulk-insert messages within the same tx. A single fsync at commit
	// instead of one per AddMessage — takes a 10k-message fork from
	// seconds to tens of ms.
	stmt, err := tx.Prepare(
		`INSERT INTO message (id, session_id, role, content, model, tokens_in, tokens_out, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("store: fork prepare: %w", err)
	}
	for _, m := range msgs {
		// Preserve original CreatedAt so audit/ordering survives
		// the fork. Using time.Now() here would collapse all
		// forked messages to the fork instant and break any
		// downstream logic that relies on monotonic ordering.
		if _, err := stmt.Exec(
			newID(), newSess.ID, m.Role, m.Content, m.Model,
			m.TokensIn, m.TokensOut, m.CreatedAt.UnixMilli(),
		); err != nil {
			stmt.Close()
			return nil, 0, fmt.Errorf("store: fork copy message: %w", err)
		}
	}
	// Close the prepared statement before committing (conventional
	// order). The defer form works too but linters prefer explicit
	// close-then-commit.
	if err := stmt.Close(); err != nil {
		return nil, 0, fmt.Errorf("store: fork close stmt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, fmt.Errorf("store: fork commit: %w", err)
	}
	return newSess, len(msgs), nil
}

// shortSessionID returns the first 8 characters of a session ID,
// for use in auto-generated fork titles. Kept as a private helper
// inside the store package so the cmd layer doesn't have to agree
// on the convention separately.
func shortSessionID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}

// GetSession retrieves a session by ID. Returns an error wrapping
// ErrSessionNotFound if the ID is not in the store so callers can
// distinguish "missing" from I/O errors via errors.Is.
func (db *DB) GetSession(id string) (*Session, error) {
	row := db.sql.QueryRow(
		`SELECT id, project_id, title, model, created_at, updated_at, summary
		 FROM session WHERE id = ?`, id,
	)
	s, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("store: get session: %w", err)
	}
	return s, nil
}

// ListSessions returns all sessions ordered by created_at descending.
func (db *DB) ListSessions() ([]*Session, error) {
	rows, err := db.sql.Query(
		`SELECT id, project_id, title, model, created_at, updated_at, summary
		 FROM session ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// LatestSession returns the most recently updated session for a project.
// Distinguishes "no rows" from real DB errors so a corrupted database
// doesn't silently start a fresh empty session — callers can act on
// errors.Is(err, sql.ErrNoRows) for the empty case and propagate any
// other error to the user.
func (db *DB) LatestSession(projectID string) (*Session, error) {
	row := db.sql.QueryRow(
		`SELECT id, project_id, title, model, created_at, updated_at, summary
		 FROM session WHERE project_id = ? ORDER BY updated_at DESC LIMIT 1`,
		projectID,
	)
	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: no sessions for project %q: %w", projectID, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("store: latest session for project %q: %w", projectID, err)
	}
	return s, nil
}

// UpdateSessionTitle updates the title and bumps updated_at.
// Exposed so the engine can backfill a title on the first user message
// of a session (the TUI creates sessions with empty titles up front).
// Routes through BackfillTitleIfEmpty so concurrent first-message
// writes don't clobber an already-set title.
func (db *DB) UpdateSessionTitle(id, title string) error {
	return db.BackfillTitleIfEmpty(id, title)
}

// updateSessionTitle unconditionally renames a session. Used by /title
// rename flows; not safe for backfill races. For race-safe backfill
// from concurrent engines use BackfillTitleIfEmpty.
func (db *DB) updateSessionTitle(id, title string) error {
	now := time.Now().UnixMilli()
	res, err := db.sql.Exec(
		`UPDATE session SET title = ?, updated_at = ? WHERE id = ?`,
		title, now, id,
	)
	if err != nil {
		return fmt.Errorf("store: update session title: %w", err)
	}
	// Don't swallow the RowsAffected error — if the driver can't
	// report it, surfacing it lets callers distinguish 'session
	// missing' from 'driver broken'.
	n, raErr := res.RowsAffected()
	if raErr != nil {
		return fmt.Errorf("store: update session title rows affected: %w", raErr)
	}
	if n == 0 {
		return fmt.Errorf("store: session %q not found", id)
	}
	return nil
}

// BackfillTitleIfEmpty is the race-safe variant used when the engine
// derives a title from the first user message. Two concurrent engines
// can both try to set a title; the WHERE clause ensures only the
// first writer wins. Once a real title exists, subsequent calls are
// silent no-ops instead of clobbering it with stale data.
func (db *DB) BackfillTitleIfEmpty(id, title string) error {
	now := time.Now().UnixMilli()
	res, err := db.sql.Exec(
		`UPDATE session SET title = ?, updated_at = ? WHERE id = ? AND (title IS NULL OR title = '')`,
		title, now, id,
	)
	if err != nil {
		return fmt.Errorf("store: backfill session title: %w", err)
	}
	if _, raErr := res.RowsAffected(); raErr != nil {
		return fmt.Errorf("store: backfill session title rows affected: %w", raErr)
	}
	return nil
}

// scanner covers both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanSession(s scanner) (*Session, error) {
	var (
		sess      Session
		createdMs int64
		updatedMs int64
	)
	err := s.Scan(
		&sess.ID, &sess.ProjectID, &sess.Title, &sess.Model,
		&createdMs, &updatedMs, &sess.Summary,
	)
	if err != nil {
		return nil, err
	}
	sess.CreatedAt = time.UnixMilli(createdMs)
	sess.UpdatedAt = time.UnixMilli(updatedMs)
	return &sess, nil
}
