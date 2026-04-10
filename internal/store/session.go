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

// GetSession retrieves a session by ID.
func (db *DB) GetSession(id string) (*Session, error) {
	row := db.sql.QueryRow(
		`SELECT id, project_id, title, model, created_at, updated_at, summary
		 FROM session WHERE id = ?`, id,
	)
	s, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: session %q not found", id)
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
func (db *DB) LatestSession(projectID string) (*Session, error) {
	row := db.sql.QueryRow(
		`SELECT id, project_id, title, model, created_at, updated_at, summary
		 FROM session WHERE project_id = ? ORDER BY updated_at DESC LIMIT 1`,
		projectID,
	)
	s, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("store: no sessions for project %q", projectID)
	}
	return s, nil
}

// UpdateSessionTitle updates the title and bumps updated_at.
// Exposed so the engine can backfill a title on the first user message
// of a session (the TUI creates sessions with empty titles up front).
func (db *DB) UpdateSessionTitle(id, title string) error {
	return db.updateSessionTitle(id, title)
}

// updateSessionTitle updates the title and bumps updated_at.
func (db *DB) updateSessionTitle(id, title string) error {
	now := time.Now().UnixMilli()
	res, err := db.sql.Exec(
		`UPDATE session SET title = ?, updated_at = ? WHERE id = ?`,
		title, now, id,
	)
	if err != nil {
		return fmt.Errorf("store: update session title: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("store: session %q not found", id)
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
