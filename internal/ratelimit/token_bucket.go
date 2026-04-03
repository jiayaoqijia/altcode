package ratelimit

import (
	"sync"
	"time"
)

// TokenBucket implements a token bucket rate limiter.
// It allows a configurable rate of operations with burst capacity.
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64       // current tokens available
	maxTokens float64      // maximum tokens (burst capacity)
	rate     float64       // tokens per second
	lastTime time.Time     // last time tokens were refilled
}

// NewTokenBucket creates a new token bucket rate limiter.
// rate specifies tokens per second, burst specifies maximum tokens.
func NewTokenBucket(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		tokens:    float64(burst),
		maxTokens: float64(burst),
		rate:      rate,
		lastTime:  time.Now(),
	}
}

// Allow returns true if a token is available, false otherwise.
// It consumes one token if available and refills based on elapsed time.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Refill tokens based on elapsed time and rate
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}

	// Check if we can consume a token
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}

	return false
}

// AllowN returns true if n tokens are available, false otherwise.
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Refill tokens based on elapsed time and rate
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}

	// Check if we can consume n tokens
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}

	return false
}

// AvailableTokens returns the current number of available tokens.
func (tb *TokenBucket) AvailableTokens() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()

	tokens := tb.tokens + elapsed*tb.rate
	if tokens > tb.maxTokens {
		tokens = tb.maxTokens
	}

	return tokens
}

// Reserve reserves n tokens and returns the wait duration until they're available.
// It returns immediately with a duration of 0 if tokens are available.
func (tb *TokenBucket) Reserve(n int) time.Duration {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Refill tokens based on elapsed time and rate
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.maxTokens {
		tb.tokens = tb.maxTokens
	}

	// If we have enough tokens, return immediately
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return 0
	}

	// Calculate wait time for required tokens
	needed := float64(n) - tb.tokens
	waitSeconds := needed / tb.rate
	tb.tokens = 0

	return time.Duration(float64(time.Second) * waitSeconds)
}
