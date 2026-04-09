package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WSMessage is a message exchanged between agents in a workspace.
type WSMessage struct {
	From      string    `json:"from"`
	To        string    `json:"to"` // role name, or "*" for broadcast
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// WSMailbox provides file-backed inter-agent messaging within a workspace.
// Stored at .altcode/workspace/{id}/mailbox.json.
type WSMailbox struct {
	mu   sync.Mutex
	path string
	msgs []*WSMessage
}

// NewWSMailbox creates a WSMailbox backed by the given file path.
func NewWSMailbox(path string) *WSMailbox {
	return &WSMailbox{path: path}
}

// Send records a message from one agent to another (or "*" for broadcast).
func (m *WSMailbox) Send(from, to, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := &WSMessage{
		From:      from,
		To:        to,
		Content:   content,
		Timestamp: time.Now(),
	}
	m.msgs = append(m.msgs, msg)
	return m.saveLocked()
}

// Receive returns all messages addressed to role (or broadcast) with a
// timestamp strictly after since.
func (m *WSMailbox) Receive(role string, since time.Time) []*WSMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*WSMessage
	for _, msg := range m.msgs {
		if !msg.Timestamp.After(since) {
			continue
		}
		if msg.To == role || msg.To == "*" {
			out = append(out, msg)
		}
	}
	return out
}

// Save persists the mailbox to disk atomically.
func (m *WSMailbox) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked()
}

func (m *WSMailbox) saveLocked() error {
	data, err := json.MarshalIndent(m.msgs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mailbox: %w", err)
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// Load reads the mailbox from disk.
func (m *WSMailbox) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			m.msgs = nil
			return nil
		}
		return fmt.Errorf("read mailbox: %w", err)
	}
	var msgs []*WSMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("unmarshal mailbox: %w", err)
	}
	m.msgs = msgs
	return nil
}
