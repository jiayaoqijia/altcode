//go:build !386 && !arm && !armbe && !mips && !mipsle && !mips64p32

// Adapted from ottie's Feishu channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/altcode-ai/altcode/gateway"
)

// Config holds Feishu bot configuration.
type Config struct {
	AppID             string
	AppSecret         string
	VerificationToken string
	EncryptKey        string
	AllowFrom         []string
	AllowAll          bool
}

// Channel implements gateway.Channel for Feishu.
type Channel struct {
	*gateway.BaseChannel
	config    Config
	client    *lark.Client
	wsClient  *larkws.Client
	botOpenID atomic.Value
	mu        sync.Mutex
	cancel    context.CancelFunc
	ctx       context.Context
	allowList []string
	allowAll  bool
}

// New creates a Feishu channel.
func New(
	cfg Config, handler gateway.MessageHandler,
) (*Channel, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf(
			"feishu app_id and app_secret are required",
		)
	}

	return &Channel{
		BaseChannel: gateway.NewBaseChannel("feishu", handler),
		config:      cfg,
		client:      lark.NewClient(cfg.AppID, cfg.AppSecret),
		allowList:   cfg.AllowFrom,
		allowAll:    cfg.AllowAll,
	}, nil
}

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	// Fetch bot open_id for @mention detection
	c.fetchBotOpenID(ctx)

	dispatcher := larkdispatcher.NewEventDispatcher(
		c.config.VerificationToken, c.config.EncryptKey,
	).OnP2MessageReceiveV1(c.handleMessageReceive)

	c.mu.Lock()
	c.wsClient = larkws.NewClient(
		c.config.AppID,
		c.config.AppSecret,
		larkws.WithEventHandler(dispatcher),
	)
	wsClient := c.wsClient
	c.mu.Unlock()

	c.SetRunning(true)

	go func() {
		if err := wsClient.Start(c.ctx); err != nil {
			_ = err
		}
	}()

	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.wsClient = nil
	c.mu.Unlock()

	c.SetRunning(false)
	return nil
}

func (c *Channel) Send(
	ctx context.Context, msg gateway.OutboundMessage,
) error {
	if !c.IsRunning() {
		return gateway.ErrNotRunning
	}

	if msg.ChatID == "" {
		return fmt.Errorf(
			"chat ID is empty: %w", gateway.ErrSendFailed,
		)
	}

	cardContent, err := buildMarkdownCard(msg.Text)
	if err != nil {
		return fmt.Errorf(
			"feishu send: card build failed: %w", err,
		)
	}
	return c.sendCard(ctx, msg.ChatID, cardContent)
}

func (c *Channel) handleMessageReceive(
	ctx context.Context,
	event *larkim.P2MessageReceiveV1,
) error {
	if event == nil || event.Event == nil ||
		event.Event.Message == nil {
		return nil
	}

	message := event.Event.Message
	sender := event.Event.Sender

	chatID := stringValue(message.ChatId)
	if chatID == "" {
		return nil
	}

	senderID := extractFeishuSenderID(sender)
	if senderID == "" {
		senderID = "unknown"
	}

	if !c.isAllowed(senderID) {
		return nil
	}

	messageType := stringValue(message.MessageType)
	messageID := stringValue(message.MessageId)
	rawContent := stringValue(message.Content)

	content := extractContent(messageType, rawContent)

	chatType := stringValue(message.ChatType)
	if chatType != "p2p" {
		// Group chat: check mention and strip placeholders
		isMentioned := c.isBotMentioned(message)
		if len(message.Mentions) > 0 {
			content = stripMentionPlaceholders(
				content, message.Mentions,
			)
		}
		if !isMentioned {
			return nil // In groups, only respond to mentions
		}
	}

	if content == "" {
		content = "[empty message]"
	}

	metadata := map[string]string{}
	if messageID != "" {
		metadata["message_id"] = messageID
	}
	if messageType != "" {
		metadata["message_type"] = messageType
	}
	if chatType != "" {
		metadata["chat_type"] = chatType
	}

	c.Handler()(ctx, gateway.InboundMessage{
		ChannelName: "feishu",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderID,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   messageID,
		Metadata:    metadata,
	})
	return nil
}

func (c *Channel) fetchBotOpenID(ctx context.Context) {
	resp, err := c.client.Do(ctx, &larkcore.ApiReq{
		HttpMethod: http.MethodGet,
		ApiPath:    "/open-apis/bot/v3/info",
		SupportedAccessTokenTypes: []larkcore.AccessTokenType{
			larkcore.AccessTokenTypeTenant,
		},
	})
	if err != nil {
		return
	}

	var result struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(resp.RawBody, &result); err != nil {
		return
	}
	if result.Code != 0 || result.Bot.OpenID == "" {
		return
	}

	c.botOpenID.Store(result.Bot.OpenID)
}

func (c *Channel) isBotMentioned(
	message *larkim.EventMessage,
) bool {
	if message.Mentions == nil {
		return false
	}

	knownID, _ := c.botOpenID.Load().(string)
	if knownID == "" {
		return false
	}

	for _, m := range message.Mentions {
		if m.Id == nil {
			continue
		}
		if m.Id.OpenId != nil && *m.Id.OpenId == knownID {
			return true
		}
	}
	return false
}

func extractContent(messageType, rawContent string) string {
	if rawContent == "" {
		return ""
	}

	switch messageType {
	case larkim.MsgTypeText:
		var textPayload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(
			[]byte(rawContent), &textPayload,
		); err == nil {
			return textPayload.Text
		}
		return rawContent
	case larkim.MsgTypePost:
		return rawContent
	case larkim.MsgTypeImage:
		return "[image]"
	case larkim.MsgTypeFile, larkim.MsgTypeAudio,
		larkim.MsgTypeMedia:
		name := extractFileName(rawContent)
		if name != "" {
			return name
		}
		return "[file]"
	default:
		return rawContent
	}
}

func (c *Channel) sendCard(
	ctx context.Context, chatID, cardContent string,
) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(larkim.MsgTypeInteractive).
			Content(cardContent).
			Build()).
		Build()

	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf(
			"feishu send card: %w", gateway.ErrTemporary,
		)
	}

	if !resp.Success() {
		return fmt.Errorf(
			"feishu api error (code=%d msg=%s): %w",
			resp.Code, resp.Msg, gateway.ErrTemporary,
		)
	}
	return nil
}

func extractFeishuSenderID(
	sender *larkim.EventSender,
) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}
	if sender.SenderId.UserId != nil &&
		*sender.SenderId.UserId != "" {
		return *sender.SenderId.UserId
	}
	if sender.SenderId.OpenId != nil &&
		*sender.SenderId.OpenId != "" {
		return *sender.SenderId.OpenId
	}
	if sender.SenderId.UnionId != nil &&
		*sender.SenderId.UnionId != "" {
		return *sender.SenderId.UnionId
	}
	return ""
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
