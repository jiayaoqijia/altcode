// Adapted from github.com/altcode-ai/altcode/gateway/manager.go
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Simplified: no media pipeline, no placeholder/typing/reaction orchestration,
// no bus pub/sub. Channels push InboundMessage to a single MessageHandler;
// outbound replies go through Manager.Send.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultRate    = 10.0
	maxRetries     = 3
	rateLimitDelay = 1 * time.Second
	baseBackoff    = 500 * time.Millisecond
	maxBackoffDur  = 8 * time.Second
)

// channelRateDefaults maps channel name to per-second rate limit.
var channelRateDefaults = map[string]float64{
	"telegram": 20,
	"slack":    1,
}

type worker struct {
	ch      Channel
	limiter *rate.Limiter
}

// Manager routes outbound messages to channel workers with rate limiting
// and retry logic.
type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
	workers  map[string]*worker
	logger   *slog.Logger
}

// NewManager creates a channel manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		workers:  make(map[string]*worker),
		logger:   logger,
	}
}

// Register adds a channel to the manager.
func (m *Manager) Register(ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.Name()] = ch
}

// StartAll starts all registered channels and their outbound workers.
func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, ch := range m.channels {
		m.logger.Info("starting channel", "channel", name)
		if err := ch.Start(ctx); err != nil {
			m.logger.Error("failed to start channel",
				"channel", name, "err", err)
			continue
		}
		w := newWorker(name, ch)
		m.workers[name] = w
	}

	m.logger.Info("all channels started",
		"count", len(m.channels))
	return nil
}

// StopAll stops all registered channels.
func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, ch := range m.channels {
		if err := ch.Stop(ctx); err != nil {
			m.logger.Error("error stopping channel",
				"channel", name, "err", err)
		}
	}

	m.logger.Info("all channels stopped")
	return nil
}

// Send enqueues an outbound message for delivery.
func (m *Manager) Send(ctx context.Context, msg OutboundMessage) error {
	m.mu.RLock()
	w, ok := m.workers[msg.Channel]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("channel %s not found", msg.Channel)
	}

	// Split if needed
	if mlp, ok := w.ch.(interface{ MaxMessageLength() int }); ok {
		maxLen := mlp.MaxMessageLength()
		if maxLen > 0 && len([]rune(msg.Text)) > maxLen {
			for _, chunk := range SplitMessage(msg.Text, maxLen) {
				chunkMsg := msg
				chunkMsg.Text = chunk
				m.sendWithRetry(ctx, msg.Channel, w, chunkMsg)
			}
			return nil
		}
	}

	m.sendWithRetry(ctx, msg.Channel, w, msg)
	return nil
}

func newWorker(name string, ch Channel) *worker {
	rateVal := defaultRate
	if r, ok := channelRateDefaults[name]; ok {
		rateVal = r
	}
	burst := int(math.Max(1, math.Ceil(rateVal/2)))

	return &worker{
		ch:      ch,
		limiter: rate.NewLimiter(rate.Limit(rateVal), burst),
	}
}

func (m *Manager) sendWithRetry(
	ctx context.Context, name string,
	w *worker, msg OutboundMessage,
) {
	if err := w.limiter.Wait(ctx); err != nil {
		return
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = w.ch.Send(ctx, msg)
		if lastErr == nil {
			return
		}

		if errors.Is(lastErr, ErrNotRunning) ||
			errors.Is(lastErr, ErrSendFailed) {
			break
		}
		if attempt == maxRetries {
			break
		}

		if errors.Is(lastErr, ErrRateLimit) {
			select {
			case <-time.After(rateLimitDelay):
				continue
			case <-ctx.Done():
				return
			}
		}

		backoff := min(
			time.Duration(
				float64(baseBackoff)*math.Pow(2, float64(attempt)),
			),
			maxBackoffDur,
		)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}
	}

	m.logger.Error("send failed",
		"channel", name,
		"chat_id", msg.ChatID,
		"err", lastErr,
		"retries", maxRetries,
	)
}

// GetStatus returns running status of all registered channels.
func (m *Manager) GetStatus() map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]bool, len(m.channels))
	for name, ch := range m.channels {
		status[name] = ch.IsRunning()
	}
	return status
}
