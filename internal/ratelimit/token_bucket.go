package ratelimit

import (
	"sync"
	"time"
)

// TokenBucket implements a token bucket rate limiter.
type TokenBucket struct {
	mu       sync.Mutex
	rate     float64      // tokens per second
	burst    int64        // maximum number of tokens
	tokens   float64      // current number of tokens
	lastTime time.Time    // last time tokens were updated
}

// NewTokenBucket creates a new token bucket rate limiter.
// rate specifies the number of tokens per second to refill.
// burst specifies the maximum number of tokens allowed.
func NewTokenBucket(rate float64, burst int64) *TokenBucket {
	return &TokenBucket{
		rate:     rate,
		burst:    burst,
		tokens:   float64(burst),
		lastTime: time.Now(),
	}
}

// Allow checks if a token is available and consumes one if it is.
// Returns true if the request is allowed, false otherwise.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Refill tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > float64(tb.burst) {
		tb.tokens = float64(tb.burst)
	}

	// Try to consume one token
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}

	return false
}

// AllowN checks if n tokens are available and consumes them if they are.
// Returns true if the request is allowed, false otherwise.
func (tb *TokenBucket) AllowN(n int64) bool {
	if n <= 0 {
		return true
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Refill tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > float64(tb.burst) {
		tb.tokens = float64(tb.burst)
	}

	// Try to consume n tokens
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}

	return false
}

// Tokens returns the current number of tokens available.
func (tb *TokenBucket) Tokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()

	tokens := tb.tokens + elapsed*tb.rate
	if tokens > float64(tb.burst) {
		tokens = float64(tb.burst)
	}

	return tokens
}

// Reset resets the token bucket to full capacity.
func (tb *TokenBucket) Reset() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.tokens = float64(tb.burst)
	tb.lastTime = time.Now()
}
