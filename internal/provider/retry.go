package provider

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
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

// retryableError lets providers signal that an error includes a
// server-specified backoff duration (Retry-After header) AND whether
// the response is a non-retryable client error.
type retryableError interface {
	error
	RetryAfter() time.Duration
	IsClientError() bool // 4xx other than 408/429 — bail immediately
}

// RetryableStream calls fn up to cfg.MaxRetries+1 times with exponential
// backoff, returning the first successful channel or the last error.
//
// Only retries errors that are KNOWN retryable: typed retryableError,
// HTTP 408/429/5xx in the error message, and clearly transient network
// errors (connection refused, dial timeout, EOF). Everything else
// (including unrecognized errors) bails immediately so test mocks
// and real client errors aren't pointlessly retried for 60+ seconds.
//
// Honors the Retry-After hint when the underlying error implements
// retryableError.
func RetryableStream(
	ctx context.Context,
	cfg RetryConfig,
	fn func(ctx context.Context) (<-chan StreamEvent, error),
) (<-chan StreamEvent, error) {
	var lastErr error
	delay := cfg.BaseDelay
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			wait := delay + time.Duration(rand.Int64N(int64(delay/2)))
			if re, ok := lastErr.(retryableError); ok {
				if hint := re.RetryAfter(); hint > 0 {
					wait = hint
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
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
		if re, ok := err.(retryableError); ok && re.IsClientError() {
			return nil, err
		}
		// Whitelist-only retry: anything not recognizably retryable
		// returns immediately. Better to surface a real error than
		// to waste 60+ seconds retrying something that will never
		// succeed (and break test mocks that expect immediate fail).
		if !isTransientError(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// isTransientError reports whether an error looks like a transient
// network or rate-limit failure that's worth retrying.
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	if re, ok := err.(retryableError); ok {
		_ = re
		return true
	}
	msg := strings.ToLower(err.Error())
	// HTTP statuses we retry: 408 timeout, 429 rate limit, 5xx server.
	for _, code := range []string{"status 408", "status 429", "status 500", "status 502", "status 503", "status 504"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	// Common transient network failures.
	for _, hint := range []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"unexpected eof",
		"timeout",
		"temporary failure",
		"i/o timeout",
		"no route to host",
	} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

