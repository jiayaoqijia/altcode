package provider

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"
)

// ErrSSEIdleTimeout fires when no bytes have arrived from the stream
// for longer than the configured idle window. Distinguishable from
// io.EOF / context.Canceled so the engine can decide whether to retry.
var ErrSSEIdleTimeout = errors.New("provider: SSE stream idle (no data received)")

// idleReader wraps an io.ReadCloser and aborts the underlying connection
// if no bytes arrive for `idle` time. This is the "stream went silent"
// recovery mechanism: half-closed TCP connections (NAT timeout, ELB
// idle disconnect, server crash without FIN) can otherwise hang the
// SSE scanner forever.
//
// The standard http.Client.Timeout caps the WHOLE request including
// the streaming response body — too aggressive for long agent turns,
// which are deliberately uncapped (see http_client.go). idleReader
// gives us the missing piece: a per-chunk deadline that resets on
// every successful Read.
type idleReader struct {
	body  io.ReadCloser
	idle  time.Duration
	timer *time.Timer
	// once-fired sentinel so Read returns ErrSSEIdleTimeout instead of
	// the underlying bytes-after-close error from net.Conn.
	tripped atomic.Bool
}

// newIdleReader wraps body with an idle deadline. The first Read arms
// the timer; every subsequent Read resets it. If the timer fires, the
// underlying body is closed (which unblocks the scanner) and Read
// returns ErrSSEIdleTimeout.
func newIdleReader(ctx context.Context, body io.ReadCloser, idle time.Duration) *idleReader {
	r := &idleReader{
		body: body,
		idle: idle,
	}
	r.timer = time.AfterFunc(idle, func() {
		r.tripped.Store(true)
		// Closing the body causes the next blocking Read on the
		// scanner to return — typically with "use of closed network
		// connection". The Read method below replaces that error
		// with ErrSSEIdleTimeout for a clean caller-side check.
		_ = body.Close()
	})
	// Tie the timer to the request context so a user cancel doesn't
	// race with the idle firing.
	go func() {
		<-ctx.Done()
		r.timer.Stop()
	}()
	return r
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		// Reset the idle deadline only on real progress.
		r.timer.Reset(r.idle)
	}
	if err != nil && r.tripped.Load() {
		return n, ErrSSEIdleTimeout
	}
	return n, err
}

func (r *idleReader) Close() error {
	r.timer.Stop()
	return r.body.Close()
}
