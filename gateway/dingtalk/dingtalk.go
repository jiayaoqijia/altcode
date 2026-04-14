// Adapted from ottie's DingTalk channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package dingtalk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"

	"github.com/altcode-ai/altcode/gateway"
)

// Config holds DingTalk bot configuration.
type Config struct {
	ClientID     string
	ClientSecret string
	AllowFrom    []string
	AllowAll     bool
}

// Channel implements gateway.Channel for DingTalk.
type Channel struct {
	*gateway.BaseChannel
	clientID        string
	clientSecret    string
	streamClient    *client.StreamClient
	ctx             context.Context
	cancel          context.CancelFunc
	allowList       []string
	allowAll        bool
	sessionWebhooks sync.Map // chatID -> sessionWebhook
}

// New creates a DingTalk channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf(
			"dingtalk client_id and client_secret are required",
		)
	}

	return &Channel{
		BaseChannel:  gateway.NewBaseChannel("dingtalk", handler),
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		allowList:    cfg.AllowFrom,
		allowAll:     cfg.AllowAll,
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int { return 20000 }

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	cred := client.NewAppCredentialConfig(c.clientID, c.clientSecret)

	c.streamClient = client.NewStreamClient(
		client.WithAppCredential(cred),
		client.WithAutoReconnect(true),
	)

	c.streamClient.RegisterChatBotCallbackRouter(
		c.onChatBotMessageReceived,
	)

	if err := c.streamClient.Start(c.ctx); err != nil {
		return fmt.Errorf("failed to start stream client: %w", err)
	}

	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	if c.streamClient != nil {
		c.streamClient.Close()
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

	sessionWebhookRaw, ok := c.sessionWebhooks.Load(msg.ChatID)
	if !ok {
		return fmt.Errorf(
			"no session_webhook found for chat %s", msg.ChatID,
		)
	}

	sessionWebhook, ok := sessionWebhookRaw.(string)
	if !ok {
		return fmt.Errorf(
			"invalid session_webhook type for chat %s", msg.ChatID,
		)
	}

	return c.sendDirectReply(ctx, sessionWebhook, msg.Text)
}

func (c *Channel) onChatBotMessageReceived(
	ctx context.Context,
	data *chatbot.BotCallbackDataModel,
) ([]byte, error) {
	content := data.Text.Content
	if content == "" {
		if contentMap, ok := data.Content.(map[string]any); ok {
			if textContent, ok := contentMap["content"].(string); ok {
				content = textContent
			}
		}
	}
	if content == "" {
		return nil, nil
	}

	senderID := data.SenderStaffId
	senderNick := data.SenderNick

	if !c.isAllowed(senderID) {
		return nil, nil
	}

	chatID := senderID
	if data.ConversationType != "1" {
		chatID = data.ConversationId
	}

	c.sessionWebhooks.Store(chatID, data.SessionWebhook)

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "dingtalk",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderNick,
		Text:        content,
		Timestamp:   time.Now(),
		Metadata: map[string]string{
			"conversation_id":   data.ConversationId,
			"conversation_type": data.ConversationType,
			"session_webhook":   data.SessionWebhook,
		},
	})

	return nil, nil
}

func (c *Channel) sendDirectReply(
	ctx context.Context, sessionWebhook, content string,
) error {
	replier := chatbot.NewChatbotReplier()

	err := replier.SimpleReplyMarkdown(
		ctx,
		sessionWebhook,
		[]byte("altcode"),
		[]byte(content),
	)
	if err != nil {
		return fmt.Errorf("dingtalk send: %w", gateway.ErrTemporary)
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
