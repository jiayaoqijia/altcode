package gateway

import "time"

// InboundMessage represents a message received from an external channel.
type InboundMessage struct {
	ChannelName string
	ChatID      string
	SenderID    string
	SenderName  string
	Text        string
	Timestamp   time.Time
	MessageID   string
	Metadata    map[string]string
}

// OutboundMessage represents a message to send back to a channel.
type OutboundMessage struct {
	Channel string
	ChatID  string
	Text    string
	ReplyTo string // optional: reply to a specific message
}
