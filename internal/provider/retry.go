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
// Honors the Retry-After hint when the underlying error implements
// retryableError, and bails immediately on 4xx client errors (except
// 408/429) instead of burning the full retry budget on a request that
// will never succeed.
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
			// Override the exponential backoff if the error gave us
			// a Retry-After hint — that's the server telling us when
			// to come back, and ignoring it gets the API token banned.
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
		// Bail immediately on non-retryable client errors (400 bad
		// request, 401 unauthorized, 403 forbidden, 404 not found).
		// 408 request timeout and 429 rate limit are still retried.
		if re, ok := err.(retryableError); ok && re.IsClientError() {
			return nil, err
		}
		// Heuristic fallback for providers that don't return a typed
		// error: prose-sniff a 4xx status string. Same exclusions as
		// the typed path.
		if isNonRetryableStatus(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// isNonRetryableStatus reports whether an error message looks like a
// 4xx HTTP status that we shouldn't retry on (400, 401, 403, 404).
// The provider error wrappers all format as "... status NNN: ...".
func isNonRetryableStatus(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"status 400", "status 401", "status 403", "status 404"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

