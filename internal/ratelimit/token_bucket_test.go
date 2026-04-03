package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestTokenBucketAllow(t *testing.T) {
	tests := []struct {
		name      string
		rate      float64
		burst     int
		operations int
		wantAllow int
	}{
		{
			name:       "allow burst",
			rate:       1.0,
			burst:      5,
			operations: 5,
			wantAllow:  5,
		},
		{
			name:       "exceed burst",
			rate:       1.0,
			burst:      3,
			operations: 5,
			wantAllow:  3,
		},
		{
			name:       "zero operations",
			rate:       1.0,
			burst:      5,
			operations: 0,
			wantAllow:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := NewTokenBucket(tt.rate, tt.burst)
			allowed := 0

			for i := 0; i < tt.operations; i++ {
				if tb.Allow() {
					allowed++
				}
			}

			if allowed != tt.wantAllow {
				t.Errorf("got %d allowed, want %d", allowed, tt.wantAllow)
			}
		})
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := NewTokenBucket(2.0, 2) // 2 tokens per second, burst of 2

	// Use up all tokens
	if !tb.Allow() || !tb.Allow() {
		t.Fatal("should have 2 tokens initially")
	}

	// Should not allow more
	if tb.Allow() {
		t.Fatal("should have no tokens left")
	}

	// Wait 1 second (should have 2 new tokens)
	time.Sleep(1 * time.Second)

	allowed := 0
	for i := 0; i < 3; i++ {
		if tb.Allow() {
			allowed++
		}
	}

	if allowed != 2 {
		t.Errorf("got %d allowed after 1 second, want 2", allowed)
	}
}

func TestTokenBucketAllowN(t *testing.T) {
	tb := NewTokenBucket(1.0, 5)

	// Try to consume 3 tokens at once
	if !tb.AllowN(3) {
		t.Fatal("should allow 3 tokens from burst of 5")
	}

	// Should have 2 tokens left
	if !tb.AllowN(2) {
		t.Fatal("should allow 2 remaining tokens")
	}

	// Should not allow 1 more
	if tb.AllowN(1) {
		t.Fatal("should not have more tokens")
	}
}

func TestTokenBucketAvailableTokens(t *testing.T) {
	tb := NewTokenBucket(1.0, 5)

	// Initially should have 5 tokens
	available := tb.AvailableTokens()
	if available != 5.0 {
		t.Errorf("got %f available tokens, want 5.0", available)
	}

	// Consume 2 tokens
	tb.Allow()
	tb.Allow()

	available = tb.AvailableTokens()
	if available != 3.0 {
		t.Errorf("got %f available tokens after consuming 2, want 3.0", available)
	}

	// Wait 1 second (should have 4 tokens: 3 remaining + 1 refilled)
	time.Sleep(1 * time.Second)

	available = tb.AvailableTokens()
	if available < 3.9 || available > 4.1 {
		t.Errorf("got %f available tokens after 1 second, want ~4.0", available)
	}
}

func TestTokenBucketReserve(t *testing.T) {
	tb := NewTokenBucket(2.0, 2)

	// Reserve 1 token (should be available)
	waitTime := tb.Reserve(1)
	if waitTime != 0 {
		t.Errorf("got wait time %v, want 0", waitTime)
	}

	// Reserve 2 more tokens (should need to wait)
	waitTime = tb.Reserve(2)
	if waitTime <= 0 {
		t.Errorf("got wait time %v, want > 0", waitTime)
	}

	// Wait time should be roughly 1 second (2 tokens at 2 tokens/second)
	expectedWait := time.Second
	tolerance := 100 * time.Millisecond
	if waitTime < expectedWait-tolerance || waitTime > expectedWait+tolerance {
		t.Errorf("got wait time %v, want ~%v (±%v)", waitTime, expectedWait, tolerance)
	}
}

func TestTokenBucketConcurrency(t *testing.T) {
	tb := NewTokenBucket(10.0, 100) // 10 tokens per second, burst of 100

	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	// Spawn 10 goroutines that each try to consume 20 tokens
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if tb.Allow() {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	// Should allow exactly 100 (the burst capacity)
	if allowed != 100 {
		t.Errorf("got %d allowed, want 100", allowed)
	}
}

func TestTokenBucketMaxTokensEnforced(t *testing.T) {
	tb := NewTokenBucket(1.0, 5)

	// Use all tokens
	for i := 0; i < 5; i++ {
		tb.Allow()
	}

	// Wait 10 seconds
	time.Sleep(10 * time.Second)

	// Should only have 5 tokens max (maxTokens enforced)
	available := tb.AvailableTokens()
	if available != 5.0 {
		t.Errorf("got %f available tokens, want 5.0 (max enforced)", available)
	}
}

func TestTokenBucketEdgeCase_VeryHighRate(t *testing.T) {
	tb := NewTokenBucket(1000.0, 10) // 1000 tokens per second

	// Should be able to refill many tokens quickly
	time.Sleep(10 * time.Millisecond)

	available := tb.AvailableTokens()
	if available < 5.0 {
		t.Errorf("got %f available tokens after 10ms at 1000 tokens/sec, want >= 5", available)
	}
}
