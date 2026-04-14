package onebot

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

func (c *Channel) connect() error {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	header := make(map[string][]string)
	if c.config.AccessToken != "" {
		header["Authorization"] = []string{
			"Bearer " + c.config.AccessToken,
		}
	}

	conn, resp, err := dialer.Dial(c.config.WSUrl, header)
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return err
	}

	conn.SetPongHandler(func(appData string) error {
		_ = conn.SetReadDeadline(
			time.Now().Add(60 * time.Second),
		)
		return nil
	})
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	go c.pinger(conn)
	return nil
}

func (c *Channel) pinger(conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.writeMu.Lock()
			err := conn.WriteMessage(
				websocket.PingMessage, nil,
			)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *Channel) reconnectLoop() {
	backoff := max(
		time.Duration(c.config.ReconnectInterval)*time.Second,
		5*time.Second,
	)
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(backoff):
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				if err := c.connect(); err == nil {
					go c.listen()
					c.fetchSelfID()
					backoff = max(
						time.Duration(c.config.ReconnectInterval)*time.Second,
						5*time.Second,
					)
				} else {
					backoff = min(backoff*2, maxBackoff)
				}
			}
		}
	}
}

func (c *Channel) listen() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				c.mu.Lock()
				if c.conn == conn {
					c.conn.Close()
					c.conn = nil
				}
				c.mu.Unlock()
				return
			}

			_ = conn.SetReadDeadline(
				time.Now().Add(60 * time.Second),
			)

			var raw oneBotRawEvent
			if err := json.Unmarshal(message, &raw); err != nil {
				continue
			}

			if raw.Echo != "" {
				c.pendingMu.Lock()
				ch, ok := c.pending[raw.Echo]
				c.pendingMu.Unlock()
				if ok {
					select {
					case ch <- message:
					default:
					}
				}
				continue
			}

			if isAPIResponse(raw.Status) {
				continue
			}

			c.handleRawEvent(&raw)
		}
	}
}

func (c *Channel) sendAPIRequest(
	action string, params any, timeout time.Duration,
) (json.RawMessage, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return nil, fmt.Errorf("WebSocket not connected")
	}

	echo := fmt.Sprintf(
		"api_%d_%d",
		time.Now().UnixNano(),
		atomic.AddInt64(&c.echoCounter, 1),
	)

	ch := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[echo] = ch
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, echo)
		c.pendingMu.Unlock()
	}()

	req := oneBotAPIRequest{
		Action: action, Params: params, Echo: echo,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to marshal API request: %w", err,
		)
	}

	c.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = conn.WriteMessage(websocket.TextMessage, data)
	_ = conn.SetWriteDeadline(time.Time{})
	c.writeMu.Unlock()

	if err != nil {
		return nil, fmt.Errorf(
			"failed to write API request: %w", err,
		)
	}

	select {
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf(
				"API request %s: channel stopped", action,
			)
		}
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf(
			"API request %s timed out", action,
		)
	case <-c.ctx.Done():
		return nil, fmt.Errorf("context canceled")
	}
}
