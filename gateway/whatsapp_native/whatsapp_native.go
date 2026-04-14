//go:build whatsapp_native

// Adapted from ottie's WhatsApp native channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.
// Uses whatsmeow for direct WhatsApp connectivity.

package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"

	"github.com/altcode-ai/altcode/gateway"
)

const (
	sqliteDriver   = "sqlite"
	whatsappDBName = "store.db"

	reconnectInitial    = 5 * time.Second
	reconnectMax        = 5 * time.Minute
	reconnectMultiplier = 2.0
)

// Config holds WhatsApp native channel configuration.
type Config struct {
	StorePath string
	AllowFrom []string
	AllowAll  bool
}

// NativeChannel implements gateway.Channel for WhatsApp native.
type NativeChannel struct {
	*gateway.BaseChannel
	storePath    string
	client       *whatsmeow.Client
	container    *sqlstore.Container
	mu           sync.Mutex
	runCtx       context.Context
	runCancel    context.CancelFunc
	reconnectMu  sync.Mutex
	reconnecting bool
	stopping     atomic.Bool
	wg           sync.WaitGroup
	allowList    []string
	allowAll     bool
}

// NewNative creates a WhatsApp native channel.
func NewNative(
	cfg Config, handler gateway.MessageHandler,
) (*NativeChannel, error) {
	storePath := cfg.StorePath
	if storePath == "" {
		storePath = "whatsapp"
	}

	return &NativeChannel{
		BaseChannel: gateway.NewBaseChannel(
			"whatsapp_native", handler,
		),
		storePath: storePath,
		allowList: cfg.AllowFrom,
		allowAll:  cfg.AllowAll,
	}, nil
}

func (c *NativeChannel) Start(ctx context.Context) error {
	c.reconnectMu.Lock()
	c.stopping.Store(false)
	c.reconnecting = false
	c.reconnectMu.Unlock()

	if err := os.MkdirAll(c.storePath, 0o700); err != nil {
		return fmt.Errorf(
			"create session store dir: %w", err,
		)
	}

	dbPath := filepath.Join(c.storePath, whatsappDBName)
	connStr := "file:" + dbPath + "?_foreign_keys=on"

	db, err := sql.Open(sqliteDriver, connStr)
	if err != nil {
		return fmt.Errorf("open whatsapp store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.ExecContext(
		ctx, "PRAGMA foreign_keys = ON",
	); err != nil {
		_ = db.Close()
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	waLogger := waLog.Stdout("WhatsApp", "WARN", true)
	container := sqlstore.NewWithDB(
		db, sqliteDriver, waLogger,
	)
	if err = container.Upgrade(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("open whatsapp store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		_ = container.Close()
		return fmt.Errorf("get device store: %w", err)
	}

	client := whatsmeow.NewClient(deviceStore, waLogger)

	c.runCtx, c.runCancel = context.WithCancel(ctx)
	client.AddEventHandler(c.eventHandler)

	c.mu.Lock()
	c.container = container
	c.client = client
	c.mu.Unlock()

	startOK := false
	defer func() {
		if startOK {
			return
		}
		c.runCancel()
		client.Disconnect()
		c.mu.Lock()
		c.client = nil
		c.container = nil
		c.mu.Unlock()
		_ = container.Close()
	}()

	if client.Store.ID == nil {
		qrChan, err := client.GetQRChannel(c.runCtx)
		if err != nil {
			return fmt.Errorf("get QR channel: %w", err)
		}
		if err := client.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		c.reconnectMu.Lock()
		if c.stopping.Load() {
			c.reconnectMu.Unlock()
			return fmt.Errorf(
				"channel stopped during QR setup",
			)
		}
		c.wg.Add(1)
		c.reconnectMu.Unlock()
		go func() {
			defer c.wg.Done()
			for {
				select {
				case <-c.runCtx.Done():
					return
				case evt, ok := <-qrChan:
					if !ok {
						return
					}
					if evt.Event == "code" {
						qrterminal.GenerateWithConfig(
							evt.Code, qrterminal.Config{
								Level:      qrterminal.L,
								Writer:     os.Stdout,
								HalfBlocks: true,
							},
						)
					}
				}
			}
		}()
	} else {
		if err := client.Connect(); err != nil {
			return fmt.Errorf("connect: %w", err)
		}
	}

	startOK = true
	c.SetRunning(true)
	return nil
}

func (c *NativeChannel) Stop(ctx context.Context) error {
	c.reconnectMu.Lock()
	c.stopping.Store(true)
	c.reconnectMu.Unlock()

	if c.runCancel != nil {
		c.runCancel()
	}

	c.mu.Lock()
	client := c.client
	container := c.container
	c.mu.Unlock()

	if client != nil {
		client.Disconnect()
	}

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}

	c.mu.Lock()
	c.client = nil
	c.container = nil
	c.mu.Unlock()

	if container != nil {
		_ = container.Close()
	}
	c.SetRunning(false)
	return nil
}

func (c *NativeChannel) eventHandler(evt any) {
	switch evt.(type) {
	case *events.Message:
		c.handleIncoming(evt.(*events.Message))
	case *events.Disconnected:
		c.reconnectMu.Lock()
		if c.reconnecting || c.stopping.Load() {
			c.reconnectMu.Unlock()
			return
		}
		c.reconnecting = true
		c.wg.Add(1)
		c.reconnectMu.Unlock()
		go func() {
			defer c.wg.Done()
			c.reconnectWithBackoff()
		}()
	}
}

func (c *NativeChannel) reconnectWithBackoff() {
	defer func() {
		c.reconnectMu.Lock()
		c.reconnecting = false
		c.reconnectMu.Unlock()
	}()

	backoff := reconnectInitial
	for {
		select {
		case <-c.runCtx.Done():
			return
		default:
		}

		c.mu.Lock()
		client := c.client
		c.mu.Unlock()
		if client == nil {
			return
		}

		if err := client.Connect(); err == nil {
			return
		}

		select {
		case <-c.runCtx.Done():
			return
		case <-time.After(backoff):
			if backoff < reconnectMax {
				next := time.Duration(
					float64(backoff) * reconnectMultiplier,
				)
				if next > reconnectMax {
					next = reconnectMax
				}
				backoff = next
			}
		}
	}
}

func (c *NativeChannel) handleIncoming(
	evt *events.Message,
) {
	if evt.Message == nil {
		return
	}
	senderID := evt.Info.Sender.String()
	chatID := evt.Info.Chat.String()

	content := evt.Message.GetConversation()
	if content == "" && evt.Message.ExtendedTextMessage != nil {
		content = evt.Message.ExtendedTextMessage.GetText()
	}
	content = strings.TrimSpace(content)

	if content == "" {
		return
	}

	if !c.isAllowed(senderID) {
		return
	}

	senderName := evt.Info.PushName

	c.Handler()(c.runCtx, gateway.InboundMessage{
		ChannelName: "whatsapp_native",
		ChatID:      chatID,
		SenderID:    senderID,
		SenderName:  senderName,
		Text:        content,
		Timestamp:   time.Now(),
		MessageID:   evt.Info.ID,
	})
}

func (c *NativeChannel) Send(
	ctx context.Context, msg gateway.OutboundMessage,
) error {
	if !c.IsRunning() {
		return gateway.ErrNotRunning
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.mu.Lock()
	client := c.client
	c.mu.Unlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf(
			"whatsapp not connected: %w", gateway.ErrTemporary,
		)
	}
	if client.Store.ID == nil {
		return fmt.Errorf(
			"whatsapp not yet paired: %w", gateway.ErrTemporary,
		)
	}

	to, err := parseJID(msg.ChatID)
	if err != nil {
		return fmt.Errorf(
			"invalid chat id %q: %w", msg.ChatID, err,
		)
	}

	waMsg := &waE2E.Message{
		Conversation: proto.String(msg.Text),
	}

	if _, err = client.SendMessage(ctx, to, waMsg); err != nil {
		return fmt.Errorf(
			"whatsapp send: %w", gateway.ErrTemporary,
		)
	}
	return nil
}

func parseJID(s string) (types.JID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.JID{}, fmt.Errorf("empty chat id")
	}
	if strings.Contains(s, "@") {
		return types.ParseJID(s)
	}
	return types.NewJID(s, types.DefaultUserServer), nil
}

func (c *NativeChannel) isAllowed(senderID string) bool {
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
