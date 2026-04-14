// Adapted from ottie's OneBot channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.
// OneBot v11 protocol over WebSocket.

package onebot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/altcode-ai/altcode/gateway"
)

// Config holds OneBot WebSocket configuration.
type Config struct {
	WSUrl              string
	AccessToken        string
	ReconnectInterval  int // seconds, 0 = no reconnect
	AllowFrom          []string
	AllowAll           bool
}

// Channel implements gateway.Channel for OneBot.
type Channel struct {
	*gateway.BaseChannel
	config        Config
	conn          *websocket.Conn
	ctx           context.Context
	cancel        context.CancelFunc
	dedup         map[string]struct{}
	dedupRing     []string
	dedupIdx      int
	mu            sync.Mutex
	writeMu       sync.Mutex
	echoCounter   int64
	selfID        int64
	pending       map[string]chan json.RawMessage
	pendingMu     sync.Mutex
	lastMessageID sync.Map
	allowList     []string
	allowAll      bool
}

type oneBotRawEvent struct {
	PostType      string          `json:"post_type"`
	MessageType   string          `json:"message_type"`
	SubType       string          `json:"sub_type"`
	MessageID     json.RawMessage `json:"message_id"`
	UserID        json.RawMessage `json:"user_id"`
	GroupID       json.RawMessage `json:"group_id"`
	RawMessage    string          `json:"raw_message"`
	Message       json.RawMessage `json:"message"`
	Sender        json.RawMessage `json:"sender"`
	SelfID        json.RawMessage `json:"self_id"`
	Time          json.RawMessage `json:"time"`
	MetaEventType string          `json:"meta_event_type"`
	NoticeType    string          `json:"notice_type"`
	Echo          string          `json:"echo"`
	RetCode       json.RawMessage `json:"retcode"`
	Status        json.RawMessage `json:"status"`
	Data          json.RawMessage `json:"data"`
}

type botStatus struct {
	Online bool `json:"online"`
	Good   bool `json:"good"`
}

type oneBotSender struct {
	UserID   json.RawMessage `json:"user_id"`
	Nickname string          `json:"nickname"`
	Card     string          `json:"card"`
}

type oneBotAPIRequest struct {
	Action string `json:"action"`
	Params any    `json:"params"`
	Echo   string `json:"echo,omitempty"`
}

type oneBotMessageSegment struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// New creates a OneBot channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	const dedupSize = 1024
	return &Channel{
		BaseChannel: gateway.NewBaseChannel("onebot", handler),
		config:      cfg,
		dedup:       make(map[string]struct{}, dedupSize),
		dedupRing:   make([]string, dedupSize),
		pending:     make(map[string]chan json.RawMessage),
		allowList:   cfg.AllowFrom,
		allowAll:    cfg.AllowAll,
	}, nil
}

func (c *Channel) Start(ctx context.Context) error {
	if c.config.WSUrl == "" {
		return fmt.Errorf("OneBot ws_url not configured")
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	if err := c.connect(); err != nil {
		if c.config.ReconnectInterval <= 0 {
			return fmt.Errorf(
				"failed to connect and reconnect disabled",
			)
		}
	} else {
		go c.listen()
		c.fetchSelfID()
	}

	if c.config.ReconnectInterval > 0 {
		go c.reconnectLoop()
	}

	c.SetRunning(true)
	return nil
}

func (c *Channel) connect() error {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	header := make(map[string][]string)
	if c.config.AccessToken != "" {
		header["Authorization"] = []string{
			"Bearer " + c.config.AccessToken,
		}
	}

	conn, resp, err := dialer.Dial(c.config.WSUrl, header)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return err
	}

	conn.SetPongHandler(func(appData string) error {
		_ = conn.SetReadDeadline(
			time.Now().Add(60 * time.Second),
		)
		return nil
	})
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	go c.pinger(conn)
	return nil
}

func (c *Channel) pinger(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.writeMu.Lock()
			err := conn.WriteMessage(
				websocket.PingMessage, nil,
			)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *Channel) fetchSelfID() {
	resp, err := c.sendAPIRequest(
		"get_login_info", nil, 5*time.Second,
	)
	if err != nil {
		return
	}

	type loginInfo struct {
		UserID   json.RawMessage `json:"user_id"`
		Nickname string          `json:"nickname"`
	}

	for _, extract := range []func() (*loginInfo, error){
		func() (*loginInfo, error) {
			var w struct {
				Data loginInfo `json:"data"`
			}
			err := json.Unmarshal(resp, &w)
			return &w.Data, err
		},
		func() (*loginInfo, error) {
			var f loginInfo
			err := json.Unmarshal(resp, &f)
			return &f, err
		},
	} {
		info, err := extract()
		if err != nil || len(info.UserID) == 0 {
			continue
		}
		if uid, err := parseJSONInt64(info.UserID); err == nil && uid > 0 {
			atomic.StoreInt64(&c.selfID, uid)
			return
		}
	}
}

func (c *Channel) sendAPIRequest(
	action string, params any, timeout time.Duration,
) (json.RawMessage, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("WebSocket not connected")
	}

	echo := fmt.Sprintf(
		"api_%d_%d",
		time.Now().UnixNano(),
		atomic.AddInt64(&c.echoCounter, 1),
	)

	ch := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[echo] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, echo)
		c.pendingMu.Unlock()
	}()

	req := oneBotAPIRequest{
		Action: action, Params: params, Echo: echo,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to marshal API request: %w", err,
		)
	}

	c.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = conn.WriteMessage(websocket.TextMessage, data)
	_ = conn.SetWriteDeadline(time.Time{})
	c.writeMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf(
			"failed to write API request: %w", err,
		)
	}

	select {
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf(
				"API request %s: channel stopped", action,
			)
		}
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf(
			"API request %s timed out", action,
		)
	case <-c.ctx.Done():
		return nil, fmt.Errorf("context canceled")
	}
}

func (c *Channel) reconnectLoop() {
	interval := max(
		time.Duration(c.config.ReconnectInterval)*time.Second,
		5*time.Second,
	)

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(interval):
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				if err := c.connect(); err == nil {
					go c.listen()
					c.fetchSelfID()
				}
			}
		}
	}
}

func (c *Channel) Stop(ctx context.Context) error {
	c.SetRunning(false)
	if c.cancel != nil {
		c.cancel()
	}

	c.pendingMu.Lock()
	for echo, ch := range c.pending {
		select {
		case ch <- nil:
		default:
		}
		delete(c.pending, echo)
	}
	c.pendingMu.Unlock()

	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()

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
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("OneBot WebSocket not connected")
	}

	action, params, err := c.buildSendRequest(msg)
	if err != nil {
		return err
	}

	echo := fmt.Sprintf(
		"send_%d", atomic.AddInt64(&c.echoCounter, 1),
	)

	req := oneBotAPIRequest{
		Action: action, Params: params, Echo: echo,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf(
			"failed to marshal OneBot request: %w", err,
		)
	}

	c.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = conn.WriteMessage(websocket.TextMessage, data)
	_ = conn.SetWriteDeadline(time.Time{})
	c.writeMu.Unlock()

	if err != nil {
		return fmt.Errorf(
			"onebot send: %w", gateway.ErrTemporary,
		)
	}
	return nil
}

func (c *Channel) buildMessageSegments(
	chatID, content string,
) []oneBotMessageSegment {
	var segments []oneBotMessageSegment

	if lastMsgID, ok := c.lastMessageID.Load(chatID); ok {
		if msgID, ok := lastMsgID.(string); ok && msgID != "" {
			segments = append(segments, oneBotMessageSegment{
				Type: "reply",
				Data: map[string]any{"id": msgID},
			})
		}
	}

	segments = append(segments, oneBotMessageSegment{
		Type: "text",
		Data: map[string]any{"text": content},
	})
	return segments
}

func (c *Channel) buildSendRequest(
	msg gateway.OutboundMessage,
) (string, any, error) {
	chatID := msg.ChatID
	segments := c.buildMessageSegments(chatID, msg.Text)

	var action, idKey, rawID string
	if rest, ok := strings.CutPrefix(chatID, "group:"); ok {
		action, idKey, rawID = "send_group_msg", "group_id", rest
	} else if rest, ok := strings.CutPrefix(chatID, "private:"); ok {
		action, idKey, rawID = "send_private_msg", "user_id", rest
	} else {
		action, idKey, rawID = "send_private_msg", "user_id", chatID
	}

	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		return "", nil, fmt.Errorf(
			"invalid %s in chatID: %s", idKey, chatID,
		)
	}
	return action, map[string]any{
		idKey: id, "message": segments,
	}, nil
}

func (c *Channel) listen() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				c.mu.Lock()
				if c.conn == conn {
					c.conn.Close()
					c.conn = nil
				}
				c.mu.Unlock()
				return
			}

			_ = conn.SetReadDeadline(
				time.Now().Add(60 * time.Second),
			)

			var raw oneBotRawEvent
			if err := json.Unmarshal(message, &raw); err != nil {
				continue
			}

			if raw.Echo != "" {
				c.pendingMu.Lock()
				ch, ok := c.pending[raw.Echo]
				c.pendingMu.Unlock()
				if ok {
					select {
					case ch <- message:
					default:
					}
				}
				continue
			}

			if isAPIResponse(raw.Status) {
				continue
			}

			c.handleRawEvent(&raw)
		}
	}
}

func isAPIResponse(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == "ok" || s == "failed"
	}
	var bs botStatus
	if json.Unmarshal(raw, &bs) == nil {
		return bs.Online || bs.Good
	}
	return false
}

func (c *Channel) handleRawEvent(raw *oneBotRawEvent) {
	switch raw.PostType {
	case "message":
		if userID, err := parseJSONInt64(raw.UserID); err == nil && userID > 0 {
			senderID := strconv.FormatInt(userID, 10)
			if !c.isAllowed(senderID) {
				return
			}
		}
		c.handleMessage(raw)
	}
}

func (c *Channel) handleMessage(raw *oneBotRawEvent) {
	userID, err := parseJSONInt64(raw.UserID)
	if err != nil {
		return
	}

	groupID, _ := parseJSONInt64(raw.GroupID)
	selfID, _ := parseJSONInt64(raw.SelfID)
	messageID := parseJSONString(raw.MessageID)

	if selfID == 0 {
		selfID = atomic.LoadInt64(&c.selfID)
	}

	parsed := c.parseMessageSegments(raw.Message, selfID)
	isBotMentioned := parsed.isBotMentioned

	content := raw.RawMessage
	if content == "" {
		content = parsed.text
	} else if selfID > 0 {
		cqAt := fmt.Sprintf("[CQ:at,qq=%d]", selfID)
		if strings.Contains(content, cqAt) {
			isBotMentioned = true
			content = strings.ReplaceAll(content, cqAt, "")
			content = strings.TrimSpace(content)
		}
	}

	if parsed.text != "" && content != parsed.text {
		content = parsed.text
	}

	var sender oneBotSender
	if len(raw.Sender) > 0 {
		_ = json.Unmarshal(raw.Sender, &sender)
	}

	if c.isDuplicate(messageID) {
		return
	}

	if content == "" {
		return
	}

	senderID := strconv.FormatInt(userID, 10)
	var chatID string

	switch raw.MessageType {
	case "private":
		chatID = "private:" + senderID
	case "group":
		groupIDStr := strconv.FormatInt(groupID, 10)
		chatID = "group:" + groupIDStr
		// In groups, only respond when mentioned
		if !isBotMentioned {
			return
		}
	default:
		return
	}

	c.lastMessageID.Store(chatID, messageID)

	senderName := sender.Nickname
	if sender.Card != "" {
		senderName = sender.Card
	}

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "onebot",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderName,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   messageID,
	})
}

type parseResult struct {
	text           string
	isBotMentioned bool
}

func (c *Channel) parseMessageSegments(
	raw json.RawMessage, selfID int64,
) parseResult {
	if len(raw) == 0 {
		return parseResult{}
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		mentioned := false
		if selfID > 0 {
			cqAt := fmt.Sprintf("[CQ:at,qq=%d]", selfID)
			if strings.Contains(s, cqAt) {
				mentioned = true
				s = strings.ReplaceAll(s, cqAt, "")
				s = strings.TrimSpace(s)
			}
		}
		return parseResult{text: s, isBotMentioned: mentioned}
	}

	var segments []map[string]any
	if err := json.Unmarshal(raw, &segments); err != nil {
		return parseResult{}
	}

	var textParts []string
	mentioned := false
	selfIDStr := strconv.FormatInt(selfID, 10)

	for _, seg := range segments {
		segType, _ := seg["type"].(string)
		data, _ := seg["data"].(map[string]any)

		switch segType {
		case "text":
			if data != nil {
				if t, ok := data["text"].(string); ok {
					textParts = append(textParts, t)
				}
			}
		case "at":
			if data != nil && selfID > 0 {
				qqVal := fmt.Sprintf("%v", data["qq"])
				if qqVal == selfIDStr || qqVal == "all" {
					mentioned = true
				}
			}
		case "image", "video", "file":
			if data != nil {
				textParts = append(
					textParts,
					fmt.Sprintf("[%s]", segType),
				)
			}
		case "record":
			textParts = append(textParts, "[voice]")
		case "face":
			if data != nil {
				faceID, _ := data["id"]
				textParts = append(
					textParts,
					fmt.Sprintf("[face:%v]", faceID),
				)
			}
		case "forward":
			textParts = append(textParts, "[forward message]")
		}
	}

	return parseResult{
		text:           strings.TrimSpace(strings.Join(textParts, "")),
		isBotMentioned: mentioned,
	}
}

func (c *Channel) isDuplicate(messageID string) bool {
	if messageID == "" || messageID == "0" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.dedup[messageID]; exists {
		return true
	}

	if old := c.dedupRing[c.dedupIdx]; old != "" {
		delete(c.dedup, old)
	}
	c.dedupRing[c.dedupIdx] = messageID
	c.dedup[messageID] = struct{}{}
	c.dedupIdx = (c.dedupIdx + 1) % len(c.dedupRing)

	return false
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

func parseJSONInt64(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strconv.ParseInt(s, 10, 64)
	}
	return 0, fmt.Errorf(
		"cannot parse as int64: %s", string(raw),
	)
}

func parseJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
