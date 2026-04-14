// Adapted from github.com/jiayaoqijia/ottie/pkg/channels (interfaces.go + base.go)
// Copyright (c) 2026 Ottie contributors — MIT License

package gateway

import (
	"context"
	"sync/atomic"
)

// Channel is the interface every messaging platform must implement.
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg OutboundMessage) error
	IsRunning() bool
}

// MessageHandler is the callback channels invoke when a message arrives.
type MessageHandler func(ctx context.Context, msg InboundMessage)

// BaseChannel provides shared plumbing for channel implementations.
type BaseChannel struct {
	name    string
	running atomic.Bool
	handler MessageHandler
}

// NewBaseChannel creates a BaseChannel with the given name and handler.
func NewBaseChannel(name string, handler MessageHandler) *BaseChannel {
	return &BaseChannel{name: name, handler: handler}
}

func (c *BaseChannel) Name() string      { return c.name }
func (c *BaseChannel) IsRunning() bool    { return c.running.Load() }
func (c *BaseChannel) SetRunning(v bool)  { c.running.Store(v) }
func (c *BaseChannel) Handler() MessageHandler { return c.handler }
