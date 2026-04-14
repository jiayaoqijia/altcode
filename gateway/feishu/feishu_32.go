//go:build 386 || arm || armbe || mips || mipsle || mips64p32

package feishu

import (
	"context"
	"errors"

	"github.com/altcode-ai/altcode/gateway"
)

// Channel is a stub for 32-bit architectures.
type Channel struct {
	*gateway.BaseChannel
}

var errUnsupported = errors.New(
	"feishu channel not supported on 32-bit architectures",
)

// New returns an error on 32-bit architectures.
func New(_ Config, _ gateway.MessageHandler) (*Channel, error) {
	return nil, errUnsupported
}

func (c *Channel) Start(_ context.Context) error {
	return errUnsupported
}

func (c *Channel) Stop(_ context.Context) error {
	return errUnsupported
}

func (c *Channel) Send(
	_ context.Context, _ gateway.OutboundMessage,
) error {
	return errUnsupported
}
