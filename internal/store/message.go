package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/altcode-ai/altcode/internal/provider"
)

// Message represents a single chat message in a session.
type Message struct {
	ID        string
	SessionID string
	Role      string
	Content   []byte
	Model     string
	TokensIn  int
	TokensOut int
	CreatedAt time.Time
}

// AddMessage inserts a new message into the given session.
func (db *DB) AddMessage(sessionID, role string, content []byte, model string, tokensIn, tokensOut int) (*Message, error) {
	m := &Message{
		ID:        newID(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Model:     model,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
		CreatedAt: time.Now(),
	}
	_, err := db.sql.Exec(
		`INSERT INTO message (id, session_id, role, content, model, tokens_in, tokens_out, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.Role, m.Content, m.Model,
		m.TokensIn, m.TokensOut, m.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("store: add message: %w", err)
	}
	return m, nil
}

// ListMessages returns all messages for a session ordered by created_at ascending.
// Tie-break on id ASC so messages persisted within the same millisecond
// (rapid streaming, race between two providers) come back in a stable
// deterministic order instead of whatever the SQLite engine fancies.
func (db *DB) ListMessages(sessionID string) ([]*Message, error) {
	rows, err := db.sql.Query(
		`SELECT id, session_id, role, content, model, tokens_in, tokens_out, created_at
		 FROM message WHERE session_id = ? ORDER BY created_at ASC, id ASC`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	defer rows.Close()

	var msgs []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// ToProviderMessages converts stored messages to provider.Message format.
func ToProviderMessages(msgs []*Message) []provider.Message {
	result := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		var pm provider.Message
		if err := json.Unmarshal(m.Content, &pm); err != nil {
			pm = provider.Message{Role: m.Role, Content: string(m.Content)}
		}
		result = append(result, pm)
	}
	return result
}

func scanMessage(s scanner) (*Message, error) {
	var (
		m         Message
		createdMs int64
	)
	err := s.Scan(
		&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Model,
		&m.TokensIn, &m.TokensOut, &createdMs,
	)
	if err != nil {
		return nil, err
	}
	m.CreatedAt = time.UnixMilli(createdMs)
	return &m, nil
}
