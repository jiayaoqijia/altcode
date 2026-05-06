// Adapted from ottie's IRC channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package irc

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"golang.org/x/time/rate"

	"github.com/jiayaoqijia/altcode/gateway"
)

// Config holds IRC connection configuration.
type Config struct {
	Server          string
	Nick            string
	User            string
	RealName        string
	Password        string
	TLS             bool
	Channels        []string // channels to join
	SASLUser        string
	SASLPassword    string
	NickServPassword string
	RequestCaps     []string
	AllowFrom       []string
	AllowAll        bool
}

// Channel implements gateway.Channel for IRC.
type Channel struct {
	*gateway.BaseChannel
	config    Config
	conn      *ircevent.Connection
	limiter   *rate.Limiter
	ctx       context.Context
	cancel    context.CancelFunc
	allowList []string
	allowAll  bool
}

// New creates an IRC channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.Server == "" {
		return nil, fmt.Errorf("irc server is required")
	}
	if cfg.Nick == "" {
		return nil, fmt.Errorf("irc nick is required")
	}

	return &Channel{
		BaseChannel: gateway.NewBaseChannel("irc", handler),
		config:      cfg,
		limiter:     rate.NewLimiter(rate.Limit(2), 2), // 2 msg/sec
		allowList:   cfg.AllowFrom,
		allowAll:    cfg.AllowAll,
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int { return 400 }

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	user := c.config.User
	if user == "" {
		user = c.config.Nick
	}
	realName := c.config.RealName
	if realName == "" {
		realName = c.config.Nick
	}
	caps := c.config.RequestCaps
	if len(caps) == 0 {
		caps = []string{"server-time", "message-tags"}
	}

	conn := &ircevent.Connection{
		Server:      c.config.Server,
		Nick:        c.config.Nick,
		User:        user,
		RealName:    realName,
		Password:    c.config.Password,
		UseTLS:      c.config.TLS,
		RequestCaps: caps,
		QuitMessage: "Goodbye",
		Debug:       false,
		Log:         nil,
	}

	if c.config.TLS {
		conn.TLSConfig = &tls.Config{
			ServerName: extractHost(c.config.Server),
		}
	}

	if c.config.SASLUser != "" && c.config.SASLPassword != "" {
		conn.SASLLogin = c.config.SASLUser
		conn.SASLPassword = c.config.SASLPassword
	}

	conn.AddConnectCallback(func(e ircmsg.Message) {
		c.onConnect(conn)
	})
	conn.AddCallback("PRIVMSG", func(e ircmsg.Message) {
		c.onPrivmsg(conn, e)
	})

	if err := conn.Connect(); err != nil {
		return fmt.Errorf("irc connect failed: %w", err)
	}

	c.conn = conn
	go conn.Loop()

	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	c.SetRunning(false)
	if c.conn != nil {
		c.conn.Quit()
	}
	if c.cancel != nil {
		c.cancel()
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

	target := msg.ChatID
	if target == "" {
		return fmt.Errorf(
			"chat ID is empty: %w", gateway.ErrSendFailed,
		)
	}
	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}

	lines := strings.Split(msg.Text, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		c.conn.Privmsg(target, line)
	}
	return nil
}

func (c *Channel) onConnect(conn *ircevent.Connection) {
	if c.config.NickServPassword != "" &&
		c.config.SASLUser == "" {
		conn.Privmsg(
			"NickServ", "IDENTIFY "+c.config.NickServPassword,
		)
	}

	for _, ch := range c.config.Channels {
		conn.Join(ch)
	}
}

func (c *Channel) onPrivmsg(
	conn *ircevent.Connection, e ircmsg.Message,
) {
	if len(e.Params) < 2 {
		return
	}

	nick := e.Nick()
	currentNick := conn.CurrentNick()

	if strings.EqualFold(nick, currentNick) {
		return
	}

	target := e.Params[0]
	content := e.Params[1]

	isDM := !strings.HasPrefix(target, "#") &&
		!strings.HasPrefix(target, "&")

	if !c.isAllowed(nick) {
		return
	}

	var chatID string
	if isDM {
		chatID = nick
	} else {
		chatID = target
		// For channel messages, check bot mention
		isMentioned := isBotMentioned(content, currentNick)
		if isMentioned {
			content = stripBotMention(content, currentNick)
		} else {
			return // In groups, only respond to mentions
		}
	}

	if strings.TrimSpace(content) == "" {
		return
	}

	messageID := fmt.Sprintf(
		"%s-%d", nick, time.Now().UnixNano(),
	)

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "irc",
		ChatID:      chatID,
		SenderID:    nick,
		SenderName:  nick,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   messageID,
		Metadata: map[string]string{
			"server": c.config.Server,
		},
	})
}

func (c *Channel) isAllowed(nick string) bool {
	if len(c.allowList) == 0 {
		return c.allowAll
	}
	for _, a := range c.allowList {
		if a == nick {
			return true
		}
	}
	return false
}

func extractHost(server string) string {
	host, _, found := strings.Cut(server, ":")
	if found {
		return host
	}
	return server
}

func isBotMentioned(content, botNick string) bool {
	lower := strings.ToLower(content)
	lowerNick := strings.ToLower(botNick)

	if strings.HasPrefix(lower, lowerNick+":") ||
		strings.HasPrefix(lower, lowerNick+",") {
		return true
	}

	idx := strings.Index(lower, lowerNick)
	if idx < 0 {
		return false
	}
	endIdx := idx + len(lowerNick)
	before := idx == 0 || (lower[idx-1] != '_' &&
		!isAlphaNum(lower[idx-1]))
	after := endIdx >= len(lower) || (lower[endIdx] != '_' &&
		!isAlphaNum(lower[endIdx]))
	return before && after
}

func stripBotMention(content, botNick string) string {
	lower := strings.ToLower(content)
	lowerNick := strings.ToLower(botNick)
	for _, sep := range []string{":", ","} {
		prefix := lowerNick + sep
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(content[len(prefix):])
		}
	}
	return content
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}
