package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucketAllow(t *testing.T) {
	// Create a bucket with 10 tokens/sec rate and burst of 5
	tb := NewTokenBucket(10.0, 5)

	// Should allow 5 requests immediately (burst capacity)
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("Request %d should be allowed (within burst)", i+1)
		}
	}

	// 6th request should be denied
	if tb.Allow() {
		t.Errorf("6th request should be denied (bucket empty)")
	}

	// Wait for 1 token to refill (100ms at 10 tokens/sec)
	time.Sleep(100 * time.Millisecond)
	if !tb.Allow() {
		t.Errorf("Request should be allowed after refill")
	}

	// Next request should be denied
	if tb.Allow() {
		t.Errorf("Second consecutive request should be denied")
	}
}

func TestTokenBucketAllowN(t *testing.T) {
	tb := NewTokenBucket(10.0, 10)

	// Should allow 10 tokens (burst)
	if !tb.AllowN(10) {
		t.Errorf("AllowN(10) should succeed with burst=10")
	}

	// Should deny 1 token (bucket empty)
	if tb.AllowN(1) {
		t.Errorf("AllowN(1) should fail when bucket is empty")
	}

	// Wait for 5 tokens
	time.Sleep(500 * time.Millisecond)

	// Should allow 5 tokens
	if !tb.AllowN(5) {
		t.Errorf("AllowN(5) should succeed after 500ms at 10 tokens/sec")
	}

	// Should deny 1 more token
	if tb.AllowN(1) {
		t.Errorf("AllowN(1) should fail when bucket is empty")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	// 1 token per second, burst of 3
	tb := NewTokenBucket(1.0, 3)

	// Use all tokens
	for i := 0; i < 3; i++ {
		if !tb.Allow() {
			t.Errorf("Initial Allow() %d should succeed", i+1)
		}
	}

	// Wait 2.5 seconds - should have 2-3 tokens (allowing for timing variance)
	time.Sleep(2500 * time.Millisecond)

	tokens := tb.Tokens()
	if tokens < 2.0 || tokens > 3.0 {
		t.Errorf("Expected 2-3 tokens after 2.5s at 1 token/sec, got %.2f", tokens)
	}
}

func TestTokenBucketReset(t *testing.T) {
	tb := NewTokenBucket(1.0, 5)

	// Consume all tokens
	for i := 0; i < 5; i++ {
		tb.Allow()
	}

	if tb.Tokens() > 0.1 {
		t.Errorf("Bucket should be empty")
	}

	// Reset
	tb.Reset()

	if tb.Tokens() < 4.9 {
		t.Errorf("Bucket should be full after Reset(), got %.2f", tb.Tokens())
	}
}

func TestTokenBucketBurstLimit(t *testing.T) {
	tb := NewTokenBucket(100.0, 10) // 100 tokens/sec, burst of 10

	// Wait a long time - should be capped at burst
	time.Sleep(1 * time.Second)

	tokens := tb.Tokens()
	if tokens > 10.1 {
		t.Errorf("Tokens should be capped at burst limit (10), got %.2f", tokens)
	}
}

func TestTokenBucketConcurrency(t *testing.T) {
	tb := NewTokenBucket(100.0, 100)

	// Launch 100 concurrent requests
	results := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			results <- tb.Allow()
		}()
	}

	// Collect results
	allowed := 0
	for i := 0; i < 100; i++ {
		if <-results {
			allowed++
		}
	}

	// Should have allowed 100 (burst capacity)
	if allowed != 100 {
		t.Errorf("Expected 100 allowed requests, got %d", allowed)
	}

	// Next request should fail
	if tb.Allow() {
		t.Errorf("Request should be denied when bucket is empty")
	}
}

func BenchmarkTokenBucketAllow(b *testing.B) {
	tb := NewTokenBucket(1000.0, 100)

	// Continuously refill to stay in a steady state
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			tb.Tokens()
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.Allow()
	}
}

func BenchmarkTokenBucketAllowN(b *testing.B) {
	tb := NewTokenBucket(1000.0, 100)

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			tb.Tokens()
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.AllowN(5)
	}
}
