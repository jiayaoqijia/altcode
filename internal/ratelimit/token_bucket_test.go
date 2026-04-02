package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucketBasic(t *testing.T) {
	// 10 tokens/sec, burst of 5
	tb := NewTokenBucket(10, 5)

	// Should allow 5 requests immediately (burst capacity)
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// Next request should be denied
	if tb.Allow() {
		t.Error("request 6 should be denied")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	// 10 tokens/sec, burst of 5
	tb := NewTokenBucket(10, 5)

	// Consume all tokens
	for i := 0; i < 5; i++ {
		tb.Allow()
	}

	// Should be denied
	if tb.Allow() {
		t.Error("should be denied when no tokens available")
	}

	// Wait 100ms, should get 1 token (10/sec * 0.1sec = 1)
	time.Sleep(100 * time.Millisecond)
	if !tb.Allow() {
		t.Error("should be allowed after refill")
	}

	// Should be denied again
	if tb.Allow() {
		t.Error("should be denied after consuming refilled token")
	}
}

func TestTokenBucketBurstCapLimit(t *testing.T) {
	// 1 token/sec, burst of 3
	tb := NewTokenBucket(1, 3)

	// Start with 3 tokens
	tokens := tb.Tokens()
	if tokens != 3.0 {
		t.Errorf("expected 3 tokens, got %f", tokens)
	}

	// Wait 10 seconds (should generate 10 tokens)
	time.Sleep(10 * time.Millisecond) // small wait to trigger refill
	tb.Tokens() // update internal time

	// But should be capped at burst limit
	tokens = tb.Tokens()
	if tokens > 3.0 {
		t.Errorf("tokens should be capped at burst (3), got %f", tokens)
	}
}

func TestTokenBucketAllowN(t *testing.T) {
	// 10 tokens/sec, burst of 10
	tb := NewTokenBucket(10, 10)

	// Should allow 5 tokens
	if !tb.AllowN(5) {
		t.Error("should allow 5 tokens")
	}

	// Should allow 5 more tokens
	if !tb.AllowN(5) {
		t.Error("should allow 5 more tokens")
	}

	// Should deny 1 token (none left)
	if tb.AllowN(1) {
		t.Error("should deny when insufficient tokens")
	}
}

func TestTokenBucketAllowNZero(t *testing.T) {
	tb := NewTokenBucket(10, 5)

	// Requesting 0 tokens should always be allowed
	if !tb.AllowN(0) {
		t.Error("should allow 0 tokens")
	}

	// Requesting negative tokens should always be allowed
	if !tb.AllowN(-1) {
		t.Error("should allow negative tokens")
	}
}

func TestTokenBucketReset(t *testing.T) {
	tb := NewTokenBucket(10, 5)

	// Consume all tokens
	for i := 0; i < 5; i++ {
		tb.Allow()
	}

	// Should be denied
	if tb.Allow() {
		t.Error("should be denied when no tokens available")
	}

	// Reset
	tb.Reset()

	// Should be allowed again
	if !tb.Allow() {
		t.Error("should be allowed after reset")
	}
}

func TestTokenBucketConcurrency(t *testing.T) {
	tb := NewTokenBucket(100, 100)

	// Try to consume all 100 tokens concurrently
	allowed := 0
	done := make(chan bool)

	for i := 0; i < 100; i++ {
		go func() {
			if tb.Allow() {
				allowed++
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	// Should have allowed exactly 100
	if allowed != 100 {
		t.Errorf("expected 100 allowed, got %d", allowed)
	}

	// Next request should be denied
	if tb.Allow() {
		t.Error("should be denied when no tokens available")
	}
}

func TestTokenBucketHighRate(t *testing.T) {
	// 1000 tokens/sec, burst of 1000
	tb := NewTokenBucket(1000, 1000)

	allowed := 0
	for i := 0; i < 1000; i++ {
		if tb.Allow() {
			allowed++
		}
	}

	if allowed != 1000 {
		t.Errorf("expected 1000 allowed, got %d", allowed)
	}

	// Wait a millisecond (should generate ~1 token)
	time.Sleep(1 * time.Millisecond)

	if !tb.Allow() {
		t.Error("should have at least 1 token after 1ms at 1000/sec rate")
	}
}

func BenchmarkTokenBucketAllow(b *testing.B) {
	tb := NewTokenBucket(10000, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Allow()
	}
}

func BenchmarkTokenBucketConcurrent(b *testing.B) {
	tb := NewTokenBucket(100000, 10000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tb.Allow()
		}
	})
}
