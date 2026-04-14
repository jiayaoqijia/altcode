package wecom

import "sync"

const wecomMaxProcessedMessages = 1000

// MessageDeduplicator provides thread-safe message deduplication.
type MessageDeduplicator struct {
	mu   sync.Mutex
	msgs map[string]bool
	ring []string
	idx  int
	max  int
}

// NewMessageDeduplicator creates a deduplicator with the given capacity.
func NewMessageDeduplicator(maxEntries int) *MessageDeduplicator {
	if maxEntries <= 0 {
		maxEntries = wecomMaxProcessedMessages
	}
	return &MessageDeduplicator{
		msgs: make(map[string]bool, maxEntries),
		ring: make([]string, maxEntries),
		max:  maxEntries,
	}
}

// MarkMessageProcessed marks msgID as processed; returns false for duplicates.
func (d *MessageDeduplicator) MarkMessageProcessed(
	msgID string,
) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.msgs[msgID] {
		return false
	}

	oldestID := d.ring[d.idx]
	if oldestID != "" {
		delete(d.msgs, oldestID)
	}

	d.msgs[msgID] = true
	d.ring[d.idx] = msgID
	d.idx = (d.idx + 1) % d.max
	return true
}
