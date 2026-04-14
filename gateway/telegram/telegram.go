// Adapted from github.com/jiayaoqijia/ottie/pkg/channels/telegram/telegram.go
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config, swarm).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.

package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/altcode-ai/altcode/gateway"
)

var (
	reHeading    = regexp.MustCompile(`^#{1,6}\s+(.+)$`)
	reBlockquote = regexp.MustCompile(`^>\s*(.*)$`)
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reBoldStar   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldUnder  = regexp.MustCompile(`__(.+?)__`)
	reItalic     = regexp.MustCompile(`_([^_]+)_`)
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reListItem   = regexp.MustCompile(`^[-*]\s+`)
	reCodeBlock  = regexp.MustCompile("```[\\w]*\\n?([\\s\\S]*?)```")
	reInlineCode = regexp.MustCompile("`([^`]+)`")
)

// Config holds Telegram bot configuration.
type Config struct {
	Token     string
	Proxy     string // optional HTTP proxy URL
	BaseURL   string // optional custom API server
	AllowFrom []string
	AllowAll  bool // must be explicitly true to allow all senders
}

// Channel implements gateway.Channel for Telegram.
type Channel struct {
	*gateway.BaseChannel
	bot       *telego.Bot
	bh        *th.BotHandler
	allowList []string
	allowAll  bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// New creates a Telegram channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	var opts []telego.BotOption

	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", cfg.Proxy, err)
		}
		opts = append(opts, telego.WithHTTPClient(&http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		}))
	} else if os.Getenv("HTTP_PROXY") != "" ||
		os.Getenv("HTTPS_PROXY") != "" {
		opts = append(opts, telego.WithHTTPClient(&http.Client{
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
		}))
	}

	if baseURL := strings.TrimRight(
		strings.TrimSpace(cfg.BaseURL), "/",
	); baseURL != "" {
		opts = append(opts, telego.WithAPIServer(baseURL))
	}

	bot, err := telego.NewBot(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	return &Channel{
		BaseChannel: gateway.NewBaseChannel("telegram", handler),
		bot:         bot,
		allowList:   cfg.AllowFrom,
		allowAll:    cfg.AllowAll,
	}, nil
}

// MaxMessageLength returns the max message length in runes.
func (c *Channel) MaxMessageLength() int { return 4000 }

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	updates, err := c.bot.UpdatesViaLongPolling(
		c.ctx, &telego.GetUpdatesParams{Timeout: 30},
	)
	if err != nil {
		c.cancel()
		return fmt.Errorf("start long polling: %w", err)
	}

	bh, err := th.NewBotHandler(c.bot, updates)
	if err != nil {
		c.cancel()
		return fmt.Errorf("create bot handler: %w", err)
	}
	c.bh = bh

	bh.HandleMessage(func(_ *th.Context, message telego.Message) error {
		return c.handleMessage(&message)
	}, th.AnyMessage())

	c.SetRunning(true)

	go func() {
		_ = bh.Start()
	}()

	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	c.SetRunning(false)
	if c.bh != nil {
		_ = c.bh.StopWithContext(ctx)
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

	chatID, threadID, err := parseChatID(msg.ChatID)
	if err != nil {
		return fmt.Errorf(
			"invalid chat ID %s: %w", msg.ChatID, gateway.ErrSendFailed,
		)
	}

	if msg.Text == "" {
		return nil
	}

	replyTo := msg.ReplyTo
	queue := []string{msg.Text}
	for len(queue) > 0 {
		chunk := queue[0]
		queue = queue[1:]

		html := markdownToTelegramHTML(chunk)

		if len([]rune(html)) > 4096 {
			ratio := float64(len([]rune(chunk))) /
				float64(len([]rune(html)))
			smallerLen := int(float64(4096) * ratio * 0.95)
			if smallerLen < 100 {
				smallerLen = 100
			}
			sub := gateway.SplitMessage(chunk, smallerLen)
			queue = append(sub, queue...)
			continue
		}

		if err := c.sendChunk(
			ctx, chatID, threadID, html, chunk, replyTo,
		); err != nil {
			return err
		}
		replyTo = ""
	}

	return nil
}

func (c *Channel) sendChunk(
	ctx context.Context, chatID int64, threadID int,
	html, mdFallback, replyTo string,
) error {
	tgMsg := tu.Message(tu.ID(chatID), html)
	tgMsg.ParseMode = telego.ModeHTML
	tgMsg.MessageThreadID = threadID

	if replyTo != "" {
		if mid, err := strconv.Atoi(replyTo); err == nil {
			tgMsg.ReplyParameters = &telego.ReplyParameters{
				MessageID: mid,
			}
		}
	}

	if _, err := c.bot.SendMessage(ctx, tgMsg); err != nil {
		tgMsg.Text = mdFallback
		tgMsg.ParseMode = ""
		if _, err = c.bot.SendMessage(ctx, tgMsg); err != nil {
			return fmt.Errorf("telegram send: %w", gateway.ErrTemporary)
		}
	}
	return nil
}

func (c *Channel) handleMessage(message *telego.Message) error {
	if message == nil || message.From == nil {
		return nil
	}

	user := message.From
	senderID := fmt.Sprintf("%d", user.ID)

	if !c.isAllowed(senderID) {
		return nil
	}

	content := message.Text
	if message.Caption != "" {
		if content != "" {
			content += "\n"
		}
		content += message.Caption
	}
	if content == "" {
		return nil
	}

	// In group chats, only respond if bot is mentioned
	if message.Chat.Type != "private" {
		if !c.isMentioned(message) {
			return nil
		}
		content = c.stripMention(content)
	}

	chatID := fmt.Sprintf("%d", message.Chat.ID)
	if message.Chat.IsForum && message.MessageThreadID != 0 {
		chatID = fmt.Sprintf(
			"%d/%d", message.Chat.ID, message.MessageThreadID,
		)
	}

	senderName := user.FirstName
	if user.Username != "" {
		senderName = user.Username
	}

	c.Handler()(c.ctx, gateway.InboundMessage{
		ChannelName: "telegram",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderName,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   fmt.Sprintf("%d", message.MessageID),
	})

	return nil
}

func (c *Channel) isAllowed(senderID string) bool {
	if len(c.allowList) == 0 {
		return c.allowAll // deny by default unless AllowAll is explicit
	}
	for _, a := range c.allowList {
		if a == senderID || strings.TrimPrefix(a, "@") == senderID {
			return true
		}
	}
	return false
}

func (c *Channel) isMentioned(message *telego.Message) bool {
	text := message.Text
	entities := message.Entities
	if text == "" {
		text = message.Caption
		entities = message.CaptionEntities
	}
	if text == "" || len(entities) == 0 {
		return false
	}

	username := c.bot.Username()
	runes := []rune(text)

	for _, e := range entities {
		if e.Offset < 0 || e.Length <= 0 {
			continue
		}
		end := e.Offset + e.Length
		if e.Offset >= len(runes) || end > len(runes) {
			continue
		}
		entityText := string(runes[e.Offset:end])

		switch e.Type {
		case telego.EntityTypeMention:
			if strings.EqualFold(entityText, "@"+username) {
				return true
			}
		case telego.EntityTypeBotCommand:
			cmd := strings.TrimPrefix(entityText, "/")
			if at := strings.IndexRune(cmd, '@'); at >= 0 {
				if strings.EqualFold(cmd[at+1:], username) {
					return true
				}
			} else {
				return true
			}
		}
	}
	return false
}

func (c *Channel) stripMention(content string) string {
	username := c.bot.Username()
	if username == "" {
		return content
	}
	re := regexp.MustCompile(`(?i)@` + regexp.QuoteMeta(username))
	return strings.TrimSpace(re.ReplaceAllString(content, ""))
}

func parseChatID(chatID string) (int64, int, error) {
	idx := strings.Index(chatID, "/")
	if idx == -1 {
		cid, err := strconv.ParseInt(chatID, 10, 64)
		return cid, 0, err
	}
	cid, err := strconv.ParseInt(chatID[:idx], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	tid, err := strconv.Atoi(chatID[idx+1:])
	if err != nil {
		return 0, 0, fmt.Errorf(
			"invalid thread ID in %q: %w", chatID, err,
		)
	}
	return cid, tid, nil
}

// --- Markdown-to-HTML conversion (from ottie) ---

func markdownToTelegramHTML(text string) string {
	if text == "" {
		return ""
	}

	cbs := extractCodeBlocks(text)
	text = cbs.text

	ics := extractInlineCodes(text)
	text = ics.text

	text = reHeading.ReplaceAllString(text, "$1")
	text = reBlockquote.ReplaceAllString(text, "$1")
	text = escapeHTML(text)
	text = reLink.ReplaceAllString(text, `<a href="$2">$1</a>`)
	text = reBoldStar.ReplaceAllString(text, "<b>$1</b>")
	text = reBoldUnder.ReplaceAllString(text, "<b>$1</b>")
	text = reItalic.ReplaceAllStringFunc(text, func(s string) string {
		m := reItalic.FindStringSubmatch(s)
		if len(m) < 2 {
			return s
		}
		return "<i>" + m[1] + "</i>"
	})
	text = reStrike.ReplaceAllString(text, "<s>$1</s>")
	text = reListItem.ReplaceAllString(text, "- ")

	for i, code := range ics.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(
			text,
			fmt.Sprintf("\x00IC%d\x00", i),
			fmt.Sprintf("<code>%s</code>", escaped),
		)
	}
	for i, code := range cbs.codes {
		escaped := escapeHTML(code)
		text = strings.ReplaceAll(
			text,
			fmt.Sprintf("\x00CB%d\x00", i),
			fmt.Sprintf("<pre><code>%s</code></pre>", escaped),
		)
	}
	return text
}

type codeMatch struct {
	text  string
	codes []string
}

func extractCodeBlocks(text string) codeMatch {
	matches := reCodeBlock.FindAllStringSubmatch(text, -1)
	codes := make([]string, 0, len(matches))
	for _, m := range matches {
		codes = append(codes, m[1])
	}
	i := 0
	text = reCodeBlock.ReplaceAllStringFunc(text, func(_ string) string {
		ph := fmt.Sprintf("\x00CB%d\x00", i)
		i++
		return ph
	})
	return codeMatch{text: text, codes: codes}
}

func extractInlineCodes(text string) codeMatch {
	matches := reInlineCode.FindAllStringSubmatch(text, -1)
	codes := make([]string, 0, len(matches))
	for _, m := range matches {
		codes = append(codes, m[1])
	}
	i := 0
	text = reInlineCode.ReplaceAllStringFunc(text, func(_ string) string {
		ph := fmt.Sprintf("\x00IC%d\x00", i)
		i++
		return ph
	})
	return codeMatch{text: text, codes: codes}
}

func escapeHTML(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}
