// Adapted from ottie's WeCom Bot channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/jiayaoqijia/altcode/gateway"
)

// Config holds WeCom Bot configuration.
type Config struct {
	Token          string
	EncodingAESKey string
	WebhookURL     string
	WebhookPath    string // defaults to "/webhook/wecom"
	ReplyTimeout   int    // seconds, default 5
	AllowFrom      []string
	AllowAll       bool
}

// WeComBotMessage represents a WeCom Bot JSON message.
type WeComBotMessage struct {
	MsgID    string `json:"msgid"`
	AIBotID  string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	ResponseURL string `json:"response_url"`
	MsgType     string `json:"msgtype"`
	Text        struct {
		Content string `json:"content"`
	} `json:"text"`
	Voice struct {
		Content string `json:"content"`
	} `json:"voice"`
	Mixed struct {
		MsgItem []struct {
			MsgType string `json:"msgtype"`
			Text    struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"msg_item"`
	} `json:"mixed"`
}

// WeComBotReplyMessage represents the reply structure.
type WeComBotReplyMessage struct {
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
}

// Channel implements gateway.Channel for WeCom Bot.
type Channel struct {
	*gateway.BaseChannel
	config        Config
	client        *http.Client
	limiter       *rate.Limiter
	ctx           context.Context
	cancel        context.CancelFunc
	processedMsgs *MessageDeduplicator
	allowList     []string
	allowAll      bool
}

// New creates a WeCom Bot channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.Token == "" || cfg.WebhookURL == "" {
		return nil, fmt.Errorf(
			"wecom token and webhook_url are required",
		)
	}

	clientTimeout := 30 * time.Second
	if d := time.Duration(cfg.ReplyTimeout) * time.Second; d > clientTimeout {
		clientTimeout = d
	}

	return &Channel{
		BaseChannel:   gateway.NewBaseChannel("wecom", handler),
		config:        cfg,
		client:        &http.Client{Timeout: clientTimeout},
		limiter:       rate.NewLimiter(rate.Limit(10), 5), // 10 msg/sec
		processedMsgs: NewMessageDeduplicator(wecomMaxProcessedMessages),
		allowList:     cfg.AllowFrom,
		allowAll:      cfg.AllowAll,
	}, nil
}

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)
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

	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	return c.sendWebhookReply(ctx, msg.ChatID, msg.Text)
}

// WebhookPath returns the webhook registration path.
func (c *Channel) WebhookPath() string {
	if c.config.WebhookPath != "" {
		return c.config.WebhookPath
	}
	return "/webhook/wecom"
}

// ServeHTTP implements http.Handler.
func (c *Channel) ServeHTTP(
	w http.ResponseWriter, r *http.Request,
) {
	if r.Method == http.MethodGet {
		c.handleVerification(w, r)
		return
	}
	if r.Method == http.MethodPost {
		c.handleMessageCallback(w, r)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (c *Channel) handleVerification(
	w http.ResponseWriter, r *http.Request,
) {
	query := r.URL.Query()
	msgSignature := query.Get("msg_signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")
	echostr := query.Get("echostr")

	if msgSignature == "" || timestamp == "" ||
		nonce == "" || echostr == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	if !verifySignature(
		c.config.Token, msgSignature, timestamp, nonce, echostr,
	) {
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	decryptedEchoStr, err := decryptMessageWithVerify(
		echostr, c.config.EncodingAESKey, "",
	)
	if err != nil {
		http.Error(
			w, "Decryption failed",
			http.StatusInternalServerError,
		)
		return
	}

	decryptedEchoStr = strings.TrimSpace(decryptedEchoStr)
	decryptedEchoStr = strings.TrimPrefix(
		decryptedEchoStr, "\xef\xbb\xbf",
	)
	w.Write([]byte(decryptedEchoStr))
}

func (c *Channel) handleMessageCallback(
	w http.ResponseWriter, r *http.Request,
) {
	query := r.URL.Query()
	msgSignature := query.Get("msg_signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")

	if msgSignature == "" || timestamp == "" || nonce == "" {
		http.Error(w, "Missing parameters", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var encryptedMsg struct {
		XMLName    xml.Name `xml:"xml"`
		ToUserName string   `xml:"ToUserName"`
		Encrypt    string   `xml:"Encrypt"`
		AgentID    string   `xml:"AgentID"`
	}

	if err = xml.Unmarshal(body, &encryptedMsg); err != nil {
		http.Error(w, "Invalid XML", http.StatusBadRequest)
		return
	}

	if !verifySignature(
		c.config.Token, msgSignature, timestamp,
		nonce, encryptedMsg.Encrypt,
	) {
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	decryptedMsg, err := decryptMessageWithVerify(
		encryptedMsg.Encrypt, c.config.EncodingAESKey, "",
	)
	if err != nil {
		http.Error(
			w, "Decryption failed",
			http.StatusInternalServerError,
		)
		return
	}

	var msg WeComBotMessage
	if err := json.Unmarshal(
		[]byte(decryptedMsg), &msg,
	); err != nil {
		http.Error(
			w, "Invalid message format", http.StatusBadRequest,
		)
		return
	}

	go c.processMessage(c.ctx, msg)

	w.Write([]byte("success"))
}

func (c *Channel) processMessage(
	ctx context.Context, msg WeComBotMessage,
) {
	if msg.MsgType != "text" && msg.MsgType != "voice" &&
		msg.MsgType != "mixed" {
		return
	}

	if !c.processedMsgs.MarkMessageProcessed(msg.MsgID) {
		return
	}

	senderID := msg.From.UserID

	if !c.isAllowed(senderID) {
		return
	}

	var chatID string
	if msg.ChatType == "group" {
		chatID = msg.ChatID
	} else {
		chatID = senderID
	}

	var content string
	switch msg.MsgType {
	case "text":
		content = msg.Text.Content
	case "voice":
		content = msg.Voice.Content
	case "mixed":
		for _, item := range msg.Mixed.MsgItem {
			if item.MsgType == "text" {
				content += item.Text.Content
			}
		}
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	c.Handler()(ctx, gateway.InboundMessage{
		ChannelName: "wecom",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderID,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   msg.MsgID,
		Metadata: map[string]string{
			"msg_type":     msg.MsgType,
			"response_url": msg.ResponseURL,
		},
	})
}

func (c *Channel) sendWebhookReply(
	ctx context.Context, userID, content string,
) error {
	reply := WeComBotReplyMessage{MsgType: "text"}
	reply.Text.Content = content

	jsonData, err := json.Marshal(reply)
	if err != nil {
		return fmt.Errorf("failed to marshal reply: %w", err)
	}

	timeout := c.config.ReplyTimeout
	if timeout <= 0 {
		timeout = 5
	}

	reqCtx, cancel := context.WithTimeout(
		ctx, time.Duration(timeout)*time.Second,
	)
	defer cancel()

	req, err := http.NewRequestWithContext(
		reqCtx, http.MethodPost, c.config.WebhookURL,
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create request: %w", err,
		)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf(
			"wecom webhook: %w", gateway.ErrTemporary,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf(
			"webhook error (%d): %s: %w",
			resp.StatusCode, string(respBody),
			gateway.ErrTemporary,
		)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf(
			"failed to parse response: %w", err,
		)
	}
	if result.ErrCode != 0 {
		return fmt.Errorf(
			"webhook API error: %s (code: %d)",
			result.ErrMsg, result.ErrCode,
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
