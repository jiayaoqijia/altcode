// Adapted from github.com/jiayaoqijia/ottie/pkg/channels/slack/slack.go
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/altcode-ai/altcode/gateway"
)

// Config holds Slack bot configuration.
type Config struct {
	BotToken  string
	AppToken  string
	AllowFrom []string
}

// Channel implements gateway.Channel for Slack.
type Channel struct {
	*gateway.BaseChannel
	api          *slack.Client
	socketClient *socketmode.Client
	botUserID    string
	allowList    []string
	ctx          context.Context
	cancel       context.CancelFunc
}

// New creates a Slack channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.BotToken == "" || cfg.AppToken == "" {
		return nil, fmt.Errorf("slack bot_token and app_token required")
	}

	api := slack.New(
		cfg.BotToken,
		slack.OptionAppLevelToken(cfg.AppToken),
	)
	socketClient := socketmode.New(api)

	return &Channel{
		BaseChannel:  gateway.NewBaseChannel("slack", handler),
		api:          api,
		socketClient: socketClient,
		allowList:    cfg.AllowFrom,
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int { return 40000 }

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	authResp, err := c.api.AuthTest()
	if err != nil {
		return fmt.Errorf("slack auth test failed: %w", err)
	}
	c.botUserID = authResp.UserID

	go c.eventLoop()

	go func() {
		if err := c.socketClient.RunContext(c.ctx); err != nil {
			if c.ctx.Err() == nil {
				// Log but don't crash
				_ = err
			}
		}
	}()

	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(_ context.Context) error {
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

	channelID, threadTS := parseChatID(msg.ChatID)
	if channelID == "" {
		return fmt.Errorf("invalid slack chat ID: %s", msg.ChatID)
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(msg.Text, false),
	}

	if msg.ReplyTo != "" && threadTS == "" {
		opts = append(opts, slack.MsgOptionTS(msg.ReplyTo))
	} else if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	_, _, err := c.api.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("slack send: %w", gateway.ErrTemporary)
	}
	return nil
}

func (c *Channel) eventLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case event, ok := <-c.socketClient.Events:
			if !ok {
				return
			}
			switch event.Type {
			case socketmode.EventTypeEventsAPI:
				c.handleEventsAPI(event)
			case socketmode.EventTypeSlashCommand:
				c.handleSlashCommand(event)
			case socketmode.EventTypeInteractive:
				if event.Request != nil {
					c.socketClient.Ack(*event.Request)
				}
			}
		}
	}
}

func (c *Channel) handleEventsAPI(event socketmode.Event) {
	if event.Request != nil {
		c.socketClient.Ack(*event.Request)
	}

	apiEvent, ok := event.Data.(slackevents.EventsAPIEvent)
	if !ok {
		return
	}

	switch ev := apiEvent.InnerEvent.Data.(type) {
	case *slackevents.MessageEvent:
		c.handleMessageEvent(ev)
	case *slackevents.AppMentionEvent:
		c.handleAppMention(ev)
	}
}

func (c *Channel) handleMessageEvent(ev *slackevents.MessageEvent) {
	if ev.User == c.botUserID || ev.User == "" || ev.BotID != "" {
		return
	}
	if ev.SubType != "" && ev.SubType != "file_share" {
		return
	}

	if !c.isAllowed(ev.User) {
		return
	}

	channelID := ev.Channel
	threadTS := ev.ThreadTimeStamp

	chatID := channelID
	if threadTS != "" {
		chatID = channelID + "/" + threadTS
	}

	content := c.stripMention(ev.Text)

	// In non-DM channels, only respond to DMs or mentions
	if !strings.HasPrefix(channelID, "D") {
		return // handled by AppMention instead
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "slack",
		ChatID:      chatID,
		SenderID:    ev.User,
		SenderName:  ev.User,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   ev.TimeStamp,
	})
}

func (c *Channel) handleAppMention(ev *slackevents.AppMentionEvent) {
	if ev.User == c.botUserID {
		return
	}
	if !c.isAllowed(ev.User) {
		return
	}

	channelID := ev.Channel
	threadTS := ev.ThreadTimeStamp
	messageTS := ev.TimeStamp

	chatID := channelID
	if threadTS != "" {
		chatID = channelID + "/" + threadTS
	} else {
		chatID = channelID + "/" + messageTS
	}

	content := c.stripMention(ev.Text)
	if strings.TrimSpace(content) == "" {
		return
	}

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "slack",
		ChatID:      chatID,
		SenderID:    ev.User,
		SenderName:  ev.User,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   messageTS,
	})
}

func (c *Channel) handleSlashCommand(event socketmode.Event) {
	cmd, ok := event.Data.(slack.SlashCommand)
	if !ok {
		return
	}
	if event.Request != nil {
		c.socketClient.Ack(*event.Request)
	}

	if !c.isAllowed(cmd.UserID) {
		return
	}

	content := cmd.Text
	if strings.TrimSpace(content) == "" {
		content = "help"
	}

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "slack",
		ChatID:      cmd.ChannelID,
		SenderID:    cmd.UserID,
		SenderName:  cmd.UserID,
		Text:        content,
		Timestamp:   time.Now(),
	})
}

func (c *Channel) isAllowed(userID string) bool {
	if len(c.allowList) == 0 {
		return true
	}
	for _, a := range c.allowList {
		if a == userID {
			return true
		}
	}
	return false
}

func (c *Channel) stripMention(text string) string {
	mention := fmt.Sprintf("<@%s>", c.botUserID)
	text = strings.ReplaceAll(text, mention, "")
	return strings.TrimSpace(text)
}

func parseChatID(chatID string) (string, string) {
	parts := strings.SplitN(chatID, "/", 2)
	channelID := parts[0]
	threadTS := ""
	if len(parts) > 1 {
		threadTS = parts[1]
	}
	return channelID, threadTS
}
