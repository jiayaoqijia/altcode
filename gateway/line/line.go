// Adapted from ottie's LINE channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.
// Uses LINE Messaging API with HTTP webhook.

package line

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/altcode-ai/altcode/gateway"
)

const (
	lineAPIBase       = "https://api.line.me/v2/bot"
	lineReplyEndpoint = lineAPIBase + "/message/reply"
	linePushEndpoint  = lineAPIBase + "/message/push"
	lineBotInfo       = lineAPIBase + "/info"

	lineReplyTokenMaxAge = 25 * time.Second
	maxWebhookBodySize   = 1 << 20 // 1 MiB
)

type replyTokenEntry struct {
	token     string
	timestamp time.Time
}

// Config holds LINE channel configuration.
type Config struct {
	ChannelSecret      string
	ChannelAccessToken string
	WebhookPath        string // defaults to "/webhook/line"
	AllowFrom          []string
	AllowAll           bool
}

// Channel implements gateway.Channel for LINE.
type Channel struct {
	*gateway.BaseChannel
	channelSecret      string
	channelAccessToken string
	webhookPath        string
	apiClient          *http.Client
	infoClient         *http.Client
	botUserID          string
	botDisplayName     string
	replyTokens        sync.Map // chatID -> replyTokenEntry
	allowList          []string
	allowAll           bool
	ctx                context.Context
	cancel             context.CancelFunc
}

// New creates a LINE channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.ChannelSecret == "" || cfg.ChannelAccessToken == "" {
		return nil, fmt.Errorf(
			"line channel_secret and channel_access_token required",
		)
	}

	webhookPath := cfg.WebhookPath
	if webhookPath == "" {
		webhookPath = "/webhook/line"
	}

	return &Channel{
		BaseChannel:        gateway.NewBaseChannel("line", handler),
		channelSecret:      cfg.ChannelSecret,
		channelAccessToken: cfg.ChannelAccessToken,
		webhookPath:        webhookPath,
		apiClient:          &http.Client{Timeout: 30 * time.Second},
		infoClient:         &http.Client{Timeout: 10 * time.Second},
		allowList:          cfg.AllowFrom,
		allowAll:           cfg.AllowAll,
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int { return 5000 }

// WebhookPath returns the path for registering on the HTTP server.
func (c *Channel) WebhookPath() string { return c.webhookPath }

// ServeHTTP implements http.Handler for the shared HTTP server.
func (c *Channel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(
		io.LimitReader(r.Body, maxWebhookBodySize+1),
	)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > maxWebhookBodySize {
		http.Error(
			w, "Request entity too large",
			http.StatusRequestEntityTooLarge,
		)
		return
	}

	signature := r.Header.Get("X-Line-Signature")
	if !c.verifySignature(body, signature) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	var payload struct {
		Events []lineEvent `json:"events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)

	for _, event := range payload.Events {
		go c.processEvent(event)
	}
}

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Fetch bot info for mention detection
	c.fetchBotInfo()

	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	c.SetRunning(false)
	return nil
}

func (c *Channel) Send(
	ctx context.Context, msg gateway.OutboundMessage,
) error {
	if !c.IsRunning() {
		return gateway.ErrNotRunning
	}

	// Try reply token first (free)
	if entry, ok := c.replyTokens.LoadAndDelete(msg.ChatID); ok {
		tokenEntry := entry.(replyTokenEntry)
		if time.Since(tokenEntry.timestamp) < lineReplyTokenMaxAge {
			if err := c.sendReply(
				ctx, tokenEntry.token, msg.Text,
			); err == nil {
				return nil
			}
		}
	}

	// Fall back to Push API
	return c.sendPush(ctx, msg.ChatID, msg.Text)
}

// --- Event types ---

type lineEvent struct {
	Type       string          `json:"type"`
	ReplyToken string          `json:"replyToken"`
	Source     lineSource      `json:"source"`
	Message    json.RawMessage `json:"message"`
	Timestamp  int64           `json:"timestamp"`
}

type lineSource struct {
	Type    string `json:"type"`
	UserID  string `json:"userId"`
	GroupID string `json:"groupId"`
	RoomID  string `json:"roomId"`
}

type lineMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

func (c *Channel) processEvent(event lineEvent) {
	if event.Type != "message" {
		return
	}

	senderID := event.Source.UserID
	chatID := c.resolveChatID(event.Source)

	var msg lineMessage
	if err := json.Unmarshal(event.Message, &msg); err != nil {
		return
	}

	// Store reply token
	if event.ReplyToken != "" {
		c.replyTokens.Store(chatID, replyTokenEntry{
			token:     event.ReplyToken,
			timestamp: time.Now(),
		})
	}

	var content string
	switch msg.Type {
	case "text":
		content = msg.Text
	case "image":
		content = "[image]"
	case "audio":
		content = "[audio]"
	case "video":
		content = "[video]"
	case "sticker":
		content = "[sticker]"
	default:
		content = fmt.Sprintf("[%s]", msg.Type)
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	if !c.isAllowed(senderID) {
		return
	}

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "line",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderID,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   msg.ID,
		Metadata: map[string]string{
			"source_type": event.Source.Type,
		},
	})
}

func (c *Channel) resolveChatID(source lineSource) string {
	switch source.Type {
	case "group":
		return source.GroupID
	case "room":
		return source.RoomID
	default:
		return source.UserID
	}
}

func (c *Channel) verifySignature(
	body []byte, signature string,
) bool {
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.channelSecret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (c *Channel) fetchBotInfo() {
	req, err := http.NewRequest(http.MethodGet, lineBotInfo, nil)
	if err != nil {
		return
	}
	req.Header.Set(
		"Authorization", "Bearer "+c.channelAccessToken,
	)

	resp, err := c.infoClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var info struct {
		UserID      string `json:"userId"`
		DisplayName string `json:"displayName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return
	}

	c.botUserID = info.UserID
	c.botDisplayName = info.DisplayName
}

func (c *Channel) sendReply(
	ctx context.Context, replyToken, content string,
) error {
	payload := map[string]any{
		"replyToken": replyToken,
		"messages": []map[string]string{{
			"type": "text",
			"text": content,
		}},
	}
	return c.callAPI(ctx, lineReplyEndpoint, payload)
}

func (c *Channel) sendPush(
	ctx context.Context, to, content string,
) error {
	payload := map[string]any{
		"to": to,
		"messages": []map[string]string{{
			"type": "text",
			"text": content,
		}},
	}
	return c.callAPI(ctx, linePushEndpoint, payload)
}

func (c *Channel) callAPI(
	ctx context.Context, endpoint string, payload any,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(
		"Authorization", "Bearer "+c.channelAccessToken,
	)

	resp, err := c.apiClient.Do(req)
	if err != nil {
		return fmt.Errorf("line api: %w", gateway.ErrTemporary)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf(
			"LINE API error (%d): %s: %w",
			resp.StatusCode, string(respBody),
			gateway.ErrTemporary,
		)
	}

	return nil
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
