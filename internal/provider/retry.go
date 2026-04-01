package provider

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// RetryConfig controls exponential backoff behaviour.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig is the standard retry policy.
var DefaultRetryConfig = RetryConfig{
	MaxRetries: 10,
	BaseDelay:  500 * time.Millisecond,
	MaxDelay:   30 * time.Second,
}

// RetryableStream calls fn up to cfg.MaxRetries+1 times with exponential
// backoff, returning the first successful channel or the last error.
func RetryableStream(
	ctx context.Context,
	cfg RetryConfig,
	fn func(ctx context.Context) (<-chan StreamEvent, error),
) (<-chan StreamEvent, error) {
	var lastErr error
	delay := cfg.BaseDelay
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			jitter := time.Duration(rand.Int64N(int64(delay / 2)))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay + jitter):
			}
			delay = min(delay*2, cfg.MaxDelay)
		}

		ch, err := fn(ctx)
		if err == nil {
			return ch, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

