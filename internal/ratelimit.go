package internal

import (
	"sync"
	"time"
)

// TokenBucket implements a thread-safe token bucket rate limiter.
// Tokens are added at a fixed rate up to a maximum burst capacity.
type TokenBucket struct {
	mu       sync.Mutex
	rate     float64   // tokens added per second
	capacity float64   // maximum tokens (burst size)
	tokens   float64   // current token count
	lastTime time.Time // last time tokens were added
}

// NewTokenBucket creates a token bucket rate limiter.
// rate: tokens added per second
// burst: maximum token capacity (allows burst up to this many requests)
func NewTokenBucket(rate float64, burst int) *TokenBucket {
	return &TokenBucket{
		rate:     rate,
		capacity: float64(burst),
		tokens:   float64(burst), // start full
		lastTime: time.Now(),
	}
}

// Allow attempts to take one token. Returns true if successful.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN attempts to take n tokens. Returns true if successful.
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.lastTime = now

	// Add tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	requested := float64(n)
	if tb.tokens >= requested {
		tb.tokens -= requested
		return true
	}
	return false
}

// Rate returns the current token addition rate (tokens per second).
func (tb *TokenBucket) Rate() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.rate
}

// Burst returns the maximum burst capacity.
func (tb *TokenBucket) Burst() int {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return int(tb.capacity)
}

// SetRate updates the token addition rate.
func (tb *TokenBucket) SetRate(rate float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.rate = rate
}

// SetBurst updates the maximum burst capacity.
func (tb *TokenBucket) SetBurst(burst int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.capacity = float64(burst)
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
}
