package ratelimiter

import (
	"sync"
	"time"
)

// TokenBucket implements a token bucket rate limiter.
// Tokens are added to the bucket at a constant rate.
// Each call to Allow() consumes one token if available.
type TokenBucket struct {
	mu        sync.Mutex
	capacity  int64       // max tokens in bucket
	tokens    int64       // current tokens
	refillRate float64    // tokens per second
	lastRefill time.Time  // last refill timestamp
}

// NewTokenBucket creates a rate limiter with the given rate (tokens/second) and burst capacity.
// Burst defines the maximum number of tokens that can be accumulated and consumed at once.
func NewTokenBucket(rate float64, burst int64) *TokenBucket {
	return &TokenBucket{
		capacity:   burst,
		tokens:     burst,
		refillRate: rate,
		lastRefill: time.Now(),
	}
}

// Allow reports whether a token is available.
// It consumes one token if available, otherwise returns false without consuming.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN reports whether n tokens are available.
// It consumes n tokens if available, otherwise returns false without consuming.
func (tb *TokenBucket) AllowN(n int64) bool {
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
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.lastRefill = now

	tokensToAdd := elapsed * tb.refillRate
	tb.tokens = min(tb.capacity, int64(float64(tb.tokens)+tokensToAdd))
}

// Capacity returns the maximum token capacity.
func (tb *TokenBucket) Capacity() int64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.capacity
}

// Available returns the current number of available tokens.
func (tb *TokenBucket) Available() int64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	return tb.tokens
}
