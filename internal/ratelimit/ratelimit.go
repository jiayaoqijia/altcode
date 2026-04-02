package ratelimit

import (
	"sync"
	"time"
)

// TokenBucket implements a token bucket rate limiter.
type TokenBucket struct {
	mu        sync.Mutex
	rate      float64   // tokens per second
	burst     float64   // maximum tokens (bucket capacity)
	tokens    float64   // current tokens
	lastTick  time.Time // last update time
}

// New creates a new token bucket rate limiter.
// rate: tokens per second
// burst: maximum tokens (bucket capacity)
func New(rate, burst float64) *TokenBucket {
	return &TokenBucket{
		rate:     rate,
		burst:    burst,
		tokens:   burst,
		lastTick: time.Now(),
	}
}

// Allow checks if a token is available and consumes one if so.
// Returns true if the request is allowed, false if rate limited.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN checks if n tokens are available and consumes them if so.
// Returns true if the request is allowed, false if rate limited.
func (tb *TokenBucket) AllowN(n int64) bool {
	if n < 1 {
		return true
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	// Check if we have enough tokens
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}

	return false
}

// refill adds tokens based on elapsed time since last refill.
// Must be called while holding the lock.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastTick).Seconds()

	// Add tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.burst {
		tb.tokens = tb.burst
	}
	tb.lastTick = now
}

// Tokens returns the current number of tokens in the bucket.
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	return tb.tokens
}

// Reset resets the bucket to full capacity.
func (tb *TokenBucket) Reset() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.tokens = tb.burst
	tb.lastTick = time.Now()
}
