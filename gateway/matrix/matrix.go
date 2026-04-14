// Adapted from ottie's Matrix channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.
// Uses mautrix for Matrix protocol support.

package matrix

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gomarkdown/markdown"
	mdhtml "github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/altcode-ai/altcode/gateway"
)

const (
	typingRefreshInterval = 20 * time.Second
	typingServerTTL       = 30 * time.Second
)

// Config holds Matrix channel configuration.
type Config struct {
	Homeserver    string
	UserID        string
	AccessToken   string
	DeviceID      string
	MessageFormat string // "plain" or "html" (default)
	JoinOnInvite  bool
	AllowFrom     []string
	AllowAll      bool
}

type typingSession struct {
	stopCh chan struct{}
	once   sync.Once
}

func newTypingSession() *typingSession {
	return &typingSession{stopCh: make(chan struct{})}
}

func (s *typingSession) stop() {
	s.once.Do(func() { close(s.stopCh) })
}

// Channel implements gateway.Channel for Matrix.
type Channel struct {
	*gateway.BaseChannel
	client    *mautrix.Client
	config    Config
	syncer    *mautrix.DefaultSyncer
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time
	allowList []string
	allowAll  bool

	typingMu       sync.Mutex
	typingSessions map[string]*typingSession
}

// New creates a Matrix channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	homeserver := strings.TrimSpace(cfg.Homeserver)
	userID := strings.TrimSpace(cfg.UserID)
	accessToken := strings.TrimSpace(cfg.AccessToken)

	if homeserver == "" {
		return nil, fmt.Errorf("matrix homeserver is required")
	}
	if userID == "" {
		return nil, fmt.Errorf("matrix user_id is required")
	}
	if accessToken == "" {
		return nil, fmt.Errorf(
			"matrix access_token is required",
		)
	}

	client, err := mautrix.NewClient(
		homeserver, id.UserID(userID), accessToken,
	)
	if err != nil {
		return nil, fmt.Errorf("create matrix client: %w", err)
	}
	if cfg.DeviceID != "" {
		client.DeviceID = id.DeviceID(cfg.DeviceID)
	}

	syncer, ok := client.Syncer.(*mautrix.DefaultSyncer)
	if !ok {
		return nil, fmt.Errorf(
			"matrix syncer is not *mautrix.DefaultSyncer",
		)
	}

	return &Channel{
		BaseChannel:    gateway.NewBaseChannel("matrix", handler),
		client:         client,
		config:         cfg,
		syncer:         syncer,
		typingSessions: make(map[string]*typingSession),
		startTime:      time.Now(),
		allowList:      cfg.AllowFrom,
		allowAll:       cfg.AllowAll,
	}, nil
}

// MaxMessageLength returns the max message length.
func (c *Channel) MaxMessageLength() int { return 65536 }

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.startTime = time.Now()

	c.syncer.OnEventType(
		event.EventMessage, c.handleMessageEvent,
	)
	if c.config.JoinOnInvite {
		c.syncer.OnEventType(
			event.StateMember, c.handleMemberEvent,
		)
	}

	c.SetRunning(true)

	go func() {
		if err := c.client.SyncWithContext(c.ctx); err != nil &&
			c.ctx.Err() == nil {
			_ = err
		}
	}()

	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	c.SetRunning(false)
	if c.cancel != nil {
		c.cancel()
	}
	c.stopTypingSessions()
	return nil
}

func (c *Channel) Send(
	ctx context.Context, msg gateway.OutboundMessage,
) error {
	if !c.IsRunning() {
		return gateway.ErrNotRunning
	}

	roomID := id.RoomID(strings.TrimSpace(msg.ChatID))
	if roomID == "" {
		return fmt.Errorf(
			"matrix room ID is empty: %w", gateway.ErrSendFailed,
		)
	}

	content := strings.TrimSpace(msg.Text)
	if content == "" {
		return nil
	}

	_, err := c.client.SendMessageEvent(
		ctx, roomID, event.EventMessage,
		c.messageContent(content),
	)
	if err != nil {
		return fmt.Errorf(
			"matrix send: %w", gateway.ErrTemporary,
		)
	}
	return nil
}

func (c *Channel) messageContent(
	text string,
) *event.MessageEventContent {
	mc := &event.MessageEventContent{
		MsgType: event.MsgText, Body: text,
	}
	if c.config.MessageFormat != "plain" {
		mc.Format = event.FormatHTML
		mc.FormattedBody = markdownToHTML(text)
	}
	return mc
}

func (c *Channel) handleMemberEvent(
	ctx context.Context, evt *event.Event,
) {
	if evt == nil {
		return
	}
	member := evt.Content.AsMember()
	if member.Membership != event.MembershipInvite {
		return
	}
	if evt.GetStateKey() != c.client.UserID.String() {
		return
	}
	_, _ = c.client.JoinRoomByID(c.baseContext(), evt.RoomID)
}

func (c *Channel) handleMessageEvent(
	ctx context.Context, evt *event.Event,
) {
	if evt == nil {
		return
	}
	if evt.Sender == c.client.UserID {
		return
	}
	if time.UnixMilli(evt.Timestamp).Before(c.startTime) {
		return
	}

	msgEvt := evt.Content.AsMessage()
	if msgEvt == nil {
		return
	}
	if msgEvt.RelatesTo != nil &&
		msgEvt.RelatesTo.GetReplaceID() != "" {
		return
	}

	var content string
	switch msgEvt.MsgType {
	case event.MsgText, event.MsgNotice:
		content = msgEvt.Body
	case event.MsgImage:
		content = "[image]"
	case event.MsgAudio:
		content = "[audio]"
	case event.MsgVideo:
		content = "[video]"
	case event.MsgFile:
		content = "[file]"
	default:
		return
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	senderID := evt.Sender.String()
	roomID := evt.RoomID.String()

	if !c.isAllowed(senderID) {
		return
	}

	// Strip self mention
	content = c.stripSelfMention(content)
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	c.Handler()(c.baseContext(), gateway.InboundMessage{
		ChannelName: "matrix",
		ChatID:      roomID,
		SenderID:    senderID,
		SenderName:  senderID,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   evt.ID.String(),
		Metadata: map[string]string{
			"room_id": roomID,
		},
	})
}

func (c *Channel) stripSelfMention(text string) string {
	userID := c.client.UserID.String()
	text = strings.ReplaceAll(text, userID, "")
	localpart := matrixLocalpart(c.client.UserID)
	if localpart != "" {
		text = strings.ReplaceAll(text, "@"+localpart, "")
	}
	return strings.TrimSpace(text)
}

func (c *Channel) stopTypingSessions() {
	c.typingMu.Lock()
	sessions := c.typingSessions
	c.typingSessions = make(map[string]*typingSession)
	c.typingMu.Unlock()

	for roomID, session := range sessions {
		session.stop()
		_, _ = c.client.UserTyping(
			context.Background(), id.RoomID(roomID), false, 0,
		)
	}
}

func (c *Channel) baseContext() context.Context {
	if c.ctx != nil {
		return c.ctx
	}
	return context.Background()
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

func markdownToHTML(md string) string {
	p := parser.NewWithExtensions(
		parser.CommonExtensions | parser.AutoHeadingIDs,
	)
	renderer := mdhtml.NewRenderer(
		mdhtml.RendererOptions{Flags: mdhtml.CommonFlags},
	)
	return strings.TrimSpace(
		string(markdown.ToHTML([]byte(md), p, renderer)),
	)
}

func matrixLocalpart(userID id.UserID) string {
	s := strings.TrimPrefix(userID.String(), "@")
	localpart, _, _ := strings.Cut(s, ":")
	return strings.TrimSpace(localpart)
}
