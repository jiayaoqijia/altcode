package agent

import (
	"sync"
	"sync/atomic"
)

// InterAgentMessage is a typed message between agents.
type InterAgentMessage struct {
	From        string // sender agent path
	To          string // recipient agent path
	Content     string
	TriggerTurn bool   // if true, wake the recipient for a new turn
	SeqNo       uint64 // monotonic sequence number
}

// Mailbox provides async inter-agent communication with ordering guarantees.
// Matches Codex's mailbox.rs pattern: send/drain/subscribe.
type Mailbox struct {
	mu       sync.Mutex
	messages []InterAgentMessage
	seq      uint64
	notify   chan struct{} // closed + recreated on each send to wake waiters
}

// NewMailbox creates a mailbox.
func NewMailbox() *Mailbox {
	return &Mailbox{notify: make(chan struct{})}
}

// Send delivers a message to the mailbox. Returns the sequence number.
func (m *Mailbox) Send(msg InterAgentMessage) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	seq := atomic.AddUint64(&m.seq, 1)
	msg.SeqNo = seq
	m.messages = append(m.messages, msg)
	// Wake any waiter
	close(m.notify)
	m.notify = make(chan struct{})
	return seq
}

// HasPending returns true if there are unread messages.
func (m *Mailbox) HasPending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages) > 0
}

// HasTriggerTurn returns true if any pending message has TriggerTurn set.
func (m *Mailbox) HasTriggerTurn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.messages {
		if msg.TriggerTurn {
			return true
		}
	}
	return false
}

// Drain returns all pending messages and clears the queue.
func (m *Mailbox) Drain() []InterAgentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := m.messages
	m.messages = nil
	return msgs
}

// Subscribe returns a channel that is closed whenever a new message arrives.
// Callers should select on this channel and call Drain() when notified.
// The channel is replaced on each send, so callers must re-subscribe.
func (m *Mailbox) Subscribe() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.notify
}

// Len returns the number of pending messages.
func (m *Mailbox) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.messages)
}
