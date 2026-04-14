// Adapted from ottie's Signal channel implementation.
// Copyright (c) 2026 Ottie contributors — MIT License
//
// Stripped ottie-specific dependencies (bus, identity, media, config).
// Wired to gateway.MessageHandler instead of bus.PublishInbound.
// Uses signal-cli JSON-RPC 2.0 API over HTTP.

package signal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/altcode-ai/altcode/gateway"
)

// Config holds Signal channel configuration.
type Config struct {
	Account     string // phone number
	AccountUUID string // for mention detection
	CLIPath     string // path to signal-cli binary
	HTTPHost    string // defaults to 127.0.0.1
	HTTPPort    int    // defaults to 8080
	AutoStart   bool   // start signal-cli daemon automatically
	AllowFrom   []string
	AllowAll    bool
}

// Channel implements gateway.Channel for Signal.
type Channel struct {
	*gateway.BaseChannel
	config     Config
	httpClient *http.Client
	baseURL    string
	daemonCmd  *daemonProcess
	ctx        context.Context
	cancel     context.CancelFunc
	rpcID      atomic.Int64
	allowList  []string
	allowAll   bool
}

// New creates a Signal channel.
func New(cfg Config, handler gateway.MessageHandler) (*Channel, error) {
	if cfg.Account == "" {
		return nil, fmt.Errorf(
			"signal account (phone number) is required",
		)
	}

	host := cfg.HTTPHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.HTTPPort
	if port == 0 {
		port = 8080
	}

	return &Channel{
		BaseChannel: gateway.NewBaseChannel("signal", handler),
		config:      cfg,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		baseURL: "http://" + net.JoinHostPort(
			host, fmt.Sprintf("%d", port),
		),
		allowList: cfg.AllowFrom,
		allowAll:  cfg.AllowAll,
	}, nil
}

func (c *Channel) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	if c.config.AutoStart {
		daemon, err := startDaemon(c.config)
		if err != nil {
			c.cancel()
			return fmt.Errorf(
				"failed to start signal-cli daemon: %w", err,
			)
		}
		c.daemonCmd = daemon

		if err := waitForDaemon(
			c.ctx, c.baseURL, 30*time.Second,
		); err != nil {
			c.stopDaemon()
			c.cancel()
			return fmt.Errorf(
				"signal-cli daemon not ready: %w", err,
			)
		}
	}

	go c.sseLoop()

	c.SetRunning(true)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	c.stopDaemon()
	c.SetRunning(false)
	return nil
}

func (c *Channel) stopDaemon() {
	if c.daemonCmd != nil {
		stopDaemon(c.daemonCmd)
		c.daemonCmd = nil
	}
}

func (c *Channel) Send(
	ctx context.Context, msg gateway.OutboundMessage,
) error {
	if !c.IsRunning() {
		return gateway.ErrNotRunning
	}

	params := map[string]any{
		"account": c.config.Account,
		"message": msg.Text,
	}

	if strings.HasPrefix(msg.ChatID, "group:") {
		groupID := strings.TrimPrefix(msg.ChatID, "group:")
		params["groupId"] = groupID
	} else {
		params["recipients"] = []string{msg.ChatID}
	}

	_, err := c.rpcCall(ctx, "send", params)
	if err != nil {
		return fmt.Errorf("signal send: %w", err)
	}
	return nil
}

// --- JSON-RPC ---

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	ID      int64  `json:"id"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Channel) rpcCall(
	ctx context.Context, method string, params any,
) (json.RawMessage, error) {
	reqID := c.rpcID.Add(1)

	reqBody := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		ID:      reqID,
		Params:  params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/api/v1/rpc"
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"rpc call returned status %d", resp.StatusCode,
		)
	}

	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf(
			"rpc error %d: %s",
			rpcResp.Error.Code, rpcResp.Error.Message,
		)
	}

	return rpcResp.Result, nil
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
