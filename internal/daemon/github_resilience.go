package daemon

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter tracks GitHub API rate limit state from response
// headers (X-RateLimit-Remaining, X-RateLimit-Reset).
type RateLimiter struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
}

// Update sets the current rate limit state. Called after every
// GitHub API response with header values.
func (r *RateLimiter) Update(remaining int, resetAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.remaining = remaining
	r.resetAt = resetAt
}

// ShouldThrottle returns true when remaining quota is below
// the safety threshold (100 requests).
func (r *RateLimiter) ShouldThrottle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.remaining < 100
}

// WaitForReset blocks until the rate limit resets or the context
// is cancelled. Returns immediately if reset is in the past.
func (r *RateLimiter) WaitForReset(ctx context.Context) error {
	r.mu.Lock()
	resetAt := r.resetAt
	r.mu.Unlock()

	delay := time.Until(resetAt)
	if delay <= 0 {
		return nil
	}

	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// retryBackoffBase is the base unit for RetryWithBackoff's exponential
// schedule. Production = 1s. Tests override via SetRetryBackoffBase.
// Karpathy autoresearch iter-3: TestRetryWithBackoff_* runs no longer
// burn 3s each on the production cadence.
var retryBackoffBase atomic.Int64 // nanoseconds; 0 means default 1s

func init() {
	retryBackoffBase.Store(int64(time.Second))
}

// SetRetryBackoffBase replaces the exponential-backoff base unit
// (returns the previous value so tests can defer-restore). Caller is
// responsible for resetting after the test if cross-test isolation
// matters; the existing backoff schedule otherwise leaks.
func SetRetryBackoffBase(d time.Duration) time.Duration {
	prev := retryBackoffBase.Swap(int64(d))
	return time.Duration(prev)
}

// RetryWithBackoff wraps fn with exponential backoff retry.
// Only transient errors (5xx, network) are retried; 4xx errors
// return immediately. Backoff caps at 30× the base.
func RetryWithBackoff(
	ctx context.Context, maxRetries int, fn func() error,
) error {
	base := time.Duration(retryBackoffBase.Load())
	if base <= 0 {
		base = time.Second
	}
	cap := 30 * base
	for i := 0; i <= maxRetries; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !isTransient(err) {
			return err
		}
		if i == maxRetries {
			break
		}
		backoff := time.Duration(1<<uint(i)) * base
		if backoff > cap {
			backoff = cap
		}
		select {
		case <-time.After(backoff):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("max retries (%d) exhausted", maxRetries)
}

// isTransient returns true for errors that indicate a temporary
// failure worth retrying: server errors, timeouts, connection
// issues.
func isTransient(err error) bool {
	s := err.Error()
	return strings.Contains(s, "502") ||
		strings.Contains(s, "503") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection refused")
}

// VerifyWebhookSignature checks an X-Hub-Signature-256 header
// against the payload using HMAC-SHA256. Returns false if secret
// is empty or signature doesn't match.
func VerifyWebhookSignature(
	payload []byte, signature, secret string,
) bool {
	if secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
