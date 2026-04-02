package ratelimit

import (
	"sync"
	"time"
)

// TokenBucket is a token bucket rate limiter.
// It allows configurable rate and burst capacity.
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	lastTime time.Time
}

// New creates a new token bucket rate limiter.
// capacity is the maximum number of tokens (burst).
// rate is tokens per second.
func New(capacity float64, rate float64) *TokenBucket {
	return &TokenBucket{
		tokens:   capacity,
		capacity: capacity,
		rate:     rate,
		lastTime: time.Now(),
	}
}

// Allow returns true if a token is available, false otherwise.
// It consumes one token if allowed.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// AllowN returns true if n tokens are available, false otherwise.
// It consumes n tokens if allowed.
func (tb *TokenBucket) AllowN(n float64) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	if tb.tokens >= n {
		tb.tokens -= n
		return true
	}
	return false
}

// refill adds tokens based on elapsed time since last refill.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Add tokens: rate tokens per second
	tokensToAdd := elapsed * tb.rate
	tb.tokens += tokensToAdd

	// Cap at capacity
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
}

// Tokens returns the current number of tokens.
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

	tb.tokens = tb.capacity
	tb.lastTime = time.Now()
}
