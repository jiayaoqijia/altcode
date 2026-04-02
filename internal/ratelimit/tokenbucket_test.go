package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	bucket := New(10, 5)

	// Should allow up to burst count
	for i := 0; i < 5; i++ {
		if !bucket.Allow() {
			t.Errorf("Expected Allow() to return true, got false on iteration %d", i)
		}
	}

	// Should deny after burst exhausted
	if bucket.Allow() {
		t.Error("Expected Allow() to return false after burst exhausted")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	bucket := New(100, 1)

	// Consume the token
	bucket.Allow()

	// Wait for refill
	time.Sleep(50 * time.Millisecond)

	// Should allow again after refill
	if !bucket.Allow() {
		t.Error("Expected Allow() to return true after refill")
	}
}

func TestTokenBucket_Concurrent(t *testing.T) {
	bucket := New(1000, 100)
	var allowed int32

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if bucket.Allow() {
					atomic.AddInt32(&allowed, 1)
				}
			}
		}()
	}

	wg.Wait()

	if allowed > 100 {
		t.Errorf("Expected at most 100 allowed, got %d", allowed)
	}
}
