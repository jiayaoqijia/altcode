// Adapted from ottie's QQ channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package qq

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
	"golang.org/x/oauth2"

	"github.com/altcode-ai/altcode/gateway"
)

const (
	dedupTTL      = 5 * time.Minute
	dedupInterval = 60 * time.Second
	dedupMaxSize  = 10000
)

// Config holds QQ bot configuration.
type Config struct {
	AppID           string
	AppSecret       string
	SendMarkdown    bool
	MaxMessageLength int
	AllowFrom       []string
	AllowAll        bool
}

// Channel implements gateway.Channel for QQ.
type Channel struct {
	*gateway.BaseChannel
	config         Config
	api            openapi.OpenAPI
	tokenSource    oauth2.TokenSource
	ctx            context.Context
	cancel         context.CancelFunc
	sessionManager botgo.SessionManager
	chatType       sync.Map
	lastMsgID      sync.Map
	msgSeqCounters sync.Map
	dedup          map[string]time.Time
	muDedup        sync.Mutex
	done           chan struct{}
	stopOnce       sync.Once
	allowList      []string
	allowAll       bool
}

// New creates a QQ channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return nil, fmt.Errorf(
			"QQ app_id and app_secret are required",
		)
	}

	return &Channel{
		BaseChannel: gateway.NewBaseChannel("qq", handler),
		config:      cfg,
		dedup:       make(map[string]time.Time),
		done:        make(chan struct{}),
		allowList:   cfg.AllowFrom,
		allowAll:    cfg.AllowAll,
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int {
	if c.config.MaxMessageLength > 0 {
		return c.config.MaxMessageLength
	}
	return 2000
}

func (c *Channel) Start(ctx context.Context) error {
	c.done = make(chan struct{})
	c.stopOnce = sync.Once{}

	credentials := &token.QQBotCredentials{
		AppID:     c.config.AppID,
		AppSecret: c.config.AppSecret,
	}
	c.tokenSource = token.NewQQBotTokenSource(credentials)

	c.ctx, c.cancel = context.WithCancel(ctx)

	if err := token.StartRefreshAccessToken(
		c.ctx, c.tokenSource,
	); err != nil {
		return fmt.Errorf(
			"failed to start token refresh: %w", err,
		)
	}

	c.api = botgo.NewOpenAPI(
		c.config.AppID, c.tokenSource,
	).WithTimeout(5 * time.Second)

	intent := event.RegisterHandlers(
		c.handleC2CMessage(),
		c.handleGroupATMessage(),
	)

	wsInfo, err := c.api.WS(c.ctx, nil, "")
	if err != nil {
		return fmt.Errorf(
			"failed to get websocket info: %w", err,
		)
	}

	c.sessionManager = botgo.NewSessionManager()

	go func() {
		if err := c.sessionManager.Start(
			wsInfo, c.tokenSource, &intent,
		); err != nil {
			c.SetRunning(false)
		}
	}()

	go c.dedupJanitor()

	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	c.SetRunning(false)
	c.stopOnce.Do(func() { close(c.done) })
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}

func (c *Channel) getChatKind(chatID string) string {
	if v, ok := c.chatType.Load(chatID); ok {
		if k, ok := v.(string); ok {
			return k
		}
	}
	return "group"
}

func (c *Channel) Send(
	ctx context.Context, msg gateway.OutboundMessage,
) error {
	if !c.IsRunning() {
		return gateway.ErrNotRunning
	}

	chatKind := c.getChatKind(msg.ChatID)

	msgToCreate := &dto.MessageToCreate{
		Content: msg.Text,
		MsgType: dto.TextMsg,
	}

	if c.config.SendMarkdown {
		msgToCreate.MsgType = dto.MarkdownMsg
		msgToCreate.Markdown = &dto.Markdown{
			Content: msg.Text,
		}
		msgToCreate.Content = ""
	}

	if v, ok := c.lastMsgID.Load(msg.ChatID); ok {
		if msgID, ok := v.(string); ok && msgID != "" {
			msgToCreate.MsgID = msgID
			if counterVal, ok := c.msgSeqCounters.Load(
				msg.ChatID,
			); ok {
				if counter, ok := counterVal.(*atomic.Uint64); ok {
					seq := counter.Add(1)
					msgToCreate.MsgSeq = uint32(seq)
				}
			}
		}
	}

	if chatKind == "group" {
		if msgToCreate.Content != "" {
			msgToCreate.Content = sanitizeURLs(
				msgToCreate.Content,
			)
		}
		if msgToCreate.Markdown != nil &&
			msgToCreate.Markdown.Content != "" {
			msgToCreate.Markdown.Content = sanitizeURLs(
				msgToCreate.Markdown.Content,
			)
		}
	}

	var err error
	if chatKind == "group" {
		_, err = c.api.PostGroupMessage(
			ctx, msg.ChatID, msgToCreate,
		)
	} else {
		_, err = c.api.PostC2CMessage(
			ctx, msg.ChatID, msgToCreate,
		)
	}

	if err != nil {
		return fmt.Errorf("qq send: %w", gateway.ErrTemporary)
	}
	return nil
}

func (c *Channel) handleC2CMessage() event.C2CMessageEventHandler {
	return func(
		event *dto.WSPayload,
		data *dto.WSC2CMessageData,
	) error {
		if c.isDuplicate(data.ID) {
			return nil
		}

		var senderID string
		if data.Author != nil && data.Author.ID != "" {
			senderID = data.Author.ID
		} else {
			return nil
		}

		content := data.Content
		if content == "" {
			return nil
		}

		if !c.isAllowed(senderID) {
			return nil
		}

		c.chatType.Store(senderID, "direct")
		c.lastMsgID.Store(senderID, data.ID)
		c.msgSeqCounters.Store(senderID, new(atomic.Uint64))

		c.Handler()(c.ctx, gateway.InboundMessage{
			ChannelName: "qq",
			ChatID:      senderID,
			SenderID:    senderID,
			SenderName:  senderID,
			Text:        content,
			Timestamp:   time.Now(),
			MessageID:   data.ID,
		})
		return nil
	}
}

func (c *Channel) handleGroupATMessage() event.GroupATMessageEventHandler {
	return func(
		event *dto.WSPayload,
		data *dto.WSGroupATMessageData,
	) error {
		if c.isDuplicate(data.ID) {
			return nil
		}

		var senderID string
		if data.Author != nil && data.Author.ID != "" {
			senderID = data.Author.ID
		} else {
			return nil
		}

		content := data.Content
		if content == "" {
			return nil
		}

		if !c.isAllowed(senderID) {
			return nil
		}

		c.chatType.Store(data.GroupID, "group")
		c.lastMsgID.Store(data.GroupID, data.ID)
		c.msgSeqCounters.Store(data.GroupID, new(atomic.Uint64))

		c.Handler()(c.ctx, gateway.InboundMessage{
			ChannelName: "qq",
			ChatID:      data.GroupID,
			SenderID:    senderID,
			SenderName:  senderID,
			Text:        content,
			Timestamp:   time.Now(),
			MessageID:   data.ID,
			Metadata: map[string]string{
				"group_id": data.GroupID,
			},
		})
		return nil
	}
}

func (c *Channel) isDuplicate(messageID string) bool {
	c.muDedup.Lock()
	defer c.muDedup.Unlock()

	if ts, exists := c.dedup[messageID]; exists &&
		time.Since(ts) < dedupTTL {
		return true
	}

	if len(c.dedup) >= dedupMaxSize {
		var oldestID string
		var oldestTS time.Time
		for id, ts := range c.dedup {
			if oldestID == "" || ts.Before(oldestTS) {
				oldestID = id
				oldestTS = ts
			}
		}
		if oldestID != "" {
			delete(c.dedup, oldestID)
		}
	}

	c.dedup[messageID] = time.Now()
	return false
}

func (c *Channel) dedupJanitor() {
	ticker := time.NewTicker(dedupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.muDedup.Lock()
			now := time.Now()
			for id, ts := range c.dedup {
				if now.Sub(ts) >= dedupTTL {
					delete(c.dedup, id)
				}
			}
			c.muDedup.Unlock()
		}
	}
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

var urlPattern = regexp.MustCompile(
	`(?i)` +
		`https?://` +
		`(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+` +
		`[a-zA-Z]{2,}` +
		`(?:[/?#]\S*)?`,
)

func sanitizeURLs(text string) string {
	return urlPattern.ReplaceAllStringFunc(
		text,
		func(match string) string {
			idx := strings.Index(match, "://")
			scheme := match[:idx+3]
			rest := match[idx+3:]

			domainEnd := len(rest)
			for i, ch := range rest {
				if ch == '/' || ch == '?' || ch == '#' {
					domainEnd = i
					break
				}
			}

			domain := rest[:domainEnd]
			path := rest[domainEnd:]
			domain = strings.ReplaceAll(domain, ".", "\u3002")
			return scheme + domain + path
		},
	)
}
