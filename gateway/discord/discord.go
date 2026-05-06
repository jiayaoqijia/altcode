// Adapted from ottie's Discord channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config, swarm).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package discord

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/jiayaoqijia/altcode/gateway"
)

const (
	sendTimeout = 10 * time.Second
)

var (
	channelRefRe = regexp.MustCompile(`<#(\d+)>`)
	msgLinkRe    = regexp.MustCompile(
		`https://(?:discord\.com|discordapp\.com)/channels/(\d+)/(\d+)/(\d+)`,
	)
)

// Config holds Discord bot configuration.
type Config struct {
	Token     string
	Proxy     string // optional HTTP proxy URL
	AllowFrom []string
	AllowAll  bool
}

// Channel implements gateway.Channel for Discord.
type Channel struct {
	*gateway.BaseChannel
	session    *discordgo.Session
	limiter    *rate.Limiter
	allowList  []string
	allowAll   bool
	ctx        context.Context
	cancel     context.CancelFunc
	typingMu   sync.Mutex
	typingStop map[string]chan struct{}
	botUserID  string
}

// New creates a Discord channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}

	if err := applyDiscordProxy(session, cfg.Proxy); err != nil {
		return nil, err
	}

	return &Channel{
		BaseChannel: gateway.NewBaseChannel("discord", handler),
		session:     session,
		limiter:     rate.NewLimiter(rate.Limit(5), 3), // 5 msg/sec
		allowList:   cfg.AllowFrom,
		allowAll:    cfg.AllowAll,
		typingStop:  make(map[string]chan struct{}),
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int { return 2000 }

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	botUser, err := c.session.User("@me")
	if err != nil {
		return fmt.Errorf("failed to get bot user: %w", err)
	}
	c.botUserID = botUser.ID

	c.session.AddHandler(c.handleMessage)

	if err := c.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}

	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	c.SetRunning(false)

	c.typingMu.Lock()
	for chatID, stop := range c.typingStop {
		close(stop)
		delete(c.typingStop, chatID)
	}
	c.typingMu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if err := c.session.Close(); err != nil {
		return fmt.Errorf("failed to close discord session: %w", err)
	}
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

	channelID := msg.ChatID
	if channelID == "" {
		return fmt.Errorf("channel ID is empty")
	}
	if msg.Text == "" {
		return nil
	}

	return c.sendChunk(ctx, channelID, msg.Text, msg.ReplyTo)
}

func (c *Channel) sendChunk(
	ctx context.Context, channelID, content, replyToID string,
) error {
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		var err error
		if replyToID != "" {
			_, err = c.session.ChannelMessageSendComplex(
				channelID,
				&discordgo.MessageSend{
					Content: content,
					Reference: &discordgo.MessageReference{
						MessageID: replyToID,
						ChannelID: channelID,
					},
				},
			)
		} else {
			_, err = c.session.ChannelMessageSend(channelID, content)
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("discord send: %w", gateway.ErrTemporary)
		}
		return nil
	case <-sendCtx.Done():
		return sendCtx.Err()
	}
}

func (c *Channel) handleMessage(
	s *discordgo.Session, m *discordgo.MessageCreate,
) {
	if m == nil || m.Author == nil {
		return
	}
	if m.Author.ID == s.State.User.ID {
		return
	}

	senderID := m.Author.ID
	if !c.isAllowed(senderID) {
		return
	}

	content := m.Content

	// In guild (group) channels, check for bot mention
	if m.GuildID != "" {
		isMentioned := false
		for _, mention := range m.Mentions {
			if mention.ID == c.botUserID {
				isMentioned = true
				break
			}
		}
		content = c.stripBotMention(content)
		if !isMentioned {
			return // In groups, only respond to mentions
		}
	} else {
		content = c.stripBotMention(content)
	}

	// Resolve Discord refs
	content = c.resolveDiscordRefs(s, content, m.GuildID)

	// Prepend referenced message content if this is a reply
	if m.MessageReference != nil && m.ReferencedMessage != nil {
		refContent := m.ReferencedMessage.Content
		if refContent != "" {
			refAuthor := "unknown"
			if m.ReferencedMessage.Author != nil {
				refAuthor = m.ReferencedMessage.Author.Username
			}
			refContent = c.resolveDiscordRefs(s, refContent, m.GuildID)
			content = fmt.Sprintf(
				"[quoted message from %s]: %s\n\n%s",
				refAuthor, refContent, content,
			)
		}
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	senderName := m.Author.Username

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "discord",
		ChatID:      m.ChannelID,
		SenderID:    senderID,
		SenderName:  senderName,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   m.ID,
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

func (c *Channel) stripBotMention(text string) string {
	if c.botUserID == "" {
		return text
	}
	text = strings.ReplaceAll(
		text, fmt.Sprintf("<@%s>", c.botUserID), "",
	)
	text = strings.ReplaceAll(
		text, fmt.Sprintf("<@!%s>", c.botUserID), "",
	)
	return strings.TrimSpace(text)
}

func (c *Channel) resolveDiscordRefs(
	s *discordgo.Session, text string, guildID string,
) string {
	text = channelRefRe.ReplaceAllStringFunc(
		text,
		func(match string) string {
			parts := channelRefRe.FindStringSubmatch(match)
			if len(parts) < 2 {
				return match
			}
			if ch, err := s.State.Channel(parts[1]); err == nil {
				return "#" + ch.Name
			}
			if ch, err := s.Channel(parts[1]); err == nil {
				return "#" + ch.Name
			}
			return match
		},
	)

	matches := msgLinkRe.FindAllStringSubmatch(text, 3)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		linkGuildID, channelID, messageID := m[1], m[2], m[3]
		if linkGuildID != guildID {
			continue
		}
		msg, err := s.ChannelMessage(channelID, messageID)
		if err != nil || msg == nil || msg.Content == "" {
			continue
		}
		author := "unknown"
		if msg.Author != nil {
			author = msg.Author.Username
		}
		text += fmt.Sprintf(
			"\n[linked message from %s]: %s", author, msg.Content,
		)
	}
	return text
}

func applyDiscordProxy(
	session *discordgo.Session, proxyAddr string,
) error {
	var proxyFunc func(*http.Request) (*url.URL, error)
	if proxyAddr != "" {
		proxyURL, err := url.Parse(proxyAddr)
		if err != nil {
			return fmt.Errorf(
				"invalid discord proxy URL %q: %w", proxyAddr, err,
			)
		}
		proxyFunc = http.ProxyURL(proxyURL)
	} else if os.Getenv("HTTP_PROXY") != "" ||
		os.Getenv("HTTPS_PROXY") != "" {
		proxyFunc = http.ProxyFromEnvironment
	}

	if proxyFunc == nil {
		return nil
	}

	transport := &http.Transport{Proxy: proxyFunc}
	session.Client = &http.Client{
		Timeout:   sendTimeout,
		Transport: transport,
	}

	if session.Dialer != nil {
		dialerCopy := *session.Dialer
		dialerCopy.Proxy = proxyFunc
		session.Dialer = &dialerCopy
	} else {
		session.Dialer = &websocket.Dialer{Proxy: proxyFunc}
	}

	return nil
}
