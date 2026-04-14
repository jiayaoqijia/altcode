// Adapted from ottie's WhatsApp channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.
// Uses WebSocket bridge for WhatsApp connectivity.

package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/altcode-ai/altcode/gateway"
)

// Config holds WhatsApp bridge configuration.
type Config struct {
	BridgeURL string // WebSocket URL to a WhatsApp bridge
	AllowFrom []string
	AllowAll  bool
}

// Channel implements gateway.Channel for WhatsApp (via bridge).
type Channel struct {
	*gateway.BaseChannel
	conn      *websocket.Conn
	url       string
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	connected bool
	allowList []string
	allowAll  bool
}

// New creates a WhatsApp channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.BridgeURL == "" {
		return nil, fmt.Errorf("whatsapp bridge_url is required")
	}

	return &Channel{
		BaseChannel: gateway.NewBaseChannel("whatsapp", handler),
		url:         cfg.BridgeURL,
		allowList:   cfg.AllowFrom,
		allowAll:    cfg.AllowAll,
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int { return 65536 }

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, resp, err := dialer.Dial(c.url, nil)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		c.cancel()
		return fmt.Errorf(
			"failed to connect to WhatsApp bridge: %w", err,
		)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	c.SetRunning(true)
	go c.listen()
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}

	c.connected = false
	c.SetRunning(false)
	return nil
}

func (c *Channel) Send(
	ctx context.Context, msg gateway.OutboundMessage,
) error {
	if !c.IsRunning() {
		return gateway.ErrNotRunning
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf(
			"whatsapp connection not established: %w",
			gateway.ErrTemporary,
		)
	}

	payload := map[string]any{
		"type":    "message",
		"to":      msg.ChatID,
		"content": msg.Text,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteMessage(
		websocket.TextMessage, data,
	); err != nil {
		_ = c.conn.SetWriteDeadline(time.Time{})
		return fmt.Errorf("whatsapp send: %w", gateway.ErrTemporary)
	}
	_ = c.conn.SetWriteDeadline(time.Time{})
	return nil
}

func (c *Channel) listen() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				time.Sleep(1 * time.Second)
				continue
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}

			var msg map[string]any
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			msgType, ok := msg["type"].(string)
			if !ok || msgType != "message" {
				continue
			}

			c.handleIncomingMessage(msg)
		}
	}
}

func (c *Channel) handleIncomingMessage(msg map[string]any) {
	senderID, ok := msg["from"].(string)
	if !ok {
		return
	}

	chatID, ok := msg["chat"].(string)
	if !ok {
		chatID = senderID
	}

	content, _ := msg["content"].(string)
	if content == "" {
		return
	}

	if !c.isAllowed(senderID) {
		return
	}

	var messageID string
	if mid, ok := msg["id"].(string); ok {
		messageID = mid
	}

	senderName := senderID
	if userName, ok := msg["from_name"].(string); ok {
		senderName = userName
	}

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "whatsapp",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderName,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   messageID,
	})
}

func (c *Channel) isAllowed(senderID string) bool {
	if len(c.allowList) == 0 {
		return c.allowAll
	}
	for _, a := range c.allowList {
		if a == senderID {
			return true
		}
	}
	return false
}
