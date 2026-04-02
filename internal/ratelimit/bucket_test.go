package ratelimit

import (
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	// 10 tokens per second, burst of 5
	tb := New(5, 10)

	// Should allow first 5 requests (burst)
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Fatalf("Allow() failed at iteration %d", i)
		}
	}

	// 6th request should fail (no tokens)
	if tb.Allow() {
		t.Fatal("Allow() should return false when out of tokens")
	}

	// Wait for tokens to refill
	time.Sleep(200 * time.Millisecond) // 2 tokens should be added

	// Should now allow 2 more requests
	if !tb.Allow() {
		t.Fatal("Allow() should return true after refill")
	}
	if !tb.Allow() {
		t.Fatal("Allow() should return true after refill")
	}

	// Should fail again
	if tb.Allow() {
		t.Fatal("Allow() should return false when out of tokens again")
	}
}

func TestAllowN(t *testing.T) {
	// 10 tokens per second, burst of 10
	tb := New(10, 10)

	// Should allow 5 tokens at once
	if !tb.AllowN(5) {
		t.Fatal("AllowN(5) failed")
	}

	// Should have 5 left (allow small float precision variation)
	tokens := tb.Tokens()
	if tokens < 4.99 || tokens > 5.01 {
		t.Fatalf("expected 5 tokens, got %v", tokens)
	}

	// Should not allow 6 tokens
	if tb.AllowN(6) {
		t.Fatal("AllowN(6) should fail with only 5 tokens")
	}

	// Should allow exactly 5
	if !tb.AllowN(5) {
		t.Fatal("AllowN(5) should succeed")
	}

	// Should be empty
	tokens = tb.Tokens()
	if tokens != 0 {
		t.Fatalf("expected 0 tokens, got %v", tokens)
	}
}

func TestRefill(t *testing.T) {
	// 1 token per second, burst of 1
	tb := New(1, 1)

	// Use the token
	if !tb.Allow() {
		t.Fatal("Allow() failed")
	}

	// Should be empty
	if tb.Allow() {
		t.Fatal("Allow() should fail when empty")
	}

	// Wait 1.1 seconds for 1 token to refill
	time.Sleep(1100 * time.Millisecond)

	// Should now have 1 token
	if !tb.Allow() {
		t.Fatal("Allow() should succeed after refill")
	}
}

func TestReset(t *testing.T) {
	tb := New(10, 5)

	// Use all tokens
	for i := 0; i < 10; i++ {
		tb.Allow()
	}

	tokens := tb.Tokens()
	if tokens >= 1 {
		t.Fatalf("expected no tokens, got %v", tokens)
	}

	// Reset
	tb.Reset()

	tokens = tb.Tokens()
	if tokens != 10 {
		t.Fatalf("expected 10 tokens after reset, got %v", tokens)
	}
}

func TestTokens(t *testing.T) {
	// 10 tokens per second, burst of 5
	tb := New(5, 10)

	tokens := tb.Tokens()
	if tokens != 5 {
		t.Fatalf("expected 5 tokens initially, got %v", tokens)
	}

	tb.Allow()
	tokens = tb.Tokens()
	if tokens != 4 {
		t.Fatalf("expected 4 tokens after Allow(), got %v", tokens)
	}

	time.Sleep(100 * time.Millisecond) // 1 token refill
	tokens = tb.Tokens()
	if tokens < 4.8 || tokens > 5 { // Allow some float precision variation
		t.Fatalf("expected ~5 tokens after refill, got %v", tokens)
	}
}

func TestConcurrency(t *testing.T) {
	// 100 tokens per second, burst of 100
	tb := New(100, 100)

	done := make(chan bool, 10)
	allowed := make(chan int, 10)

	// 10 goroutines trying to get tokens
	for i := 0; i < 10; i++ {
		go func() {
			count := 0
			for j := 0; j < 20; j++ {
				if tb.Allow() {
					count++
				}
			}
			allowed <- count
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Sum up all tokens that were allowed
	total := 0
	for i := 0; i < 10; i++ {
		total += <-allowed
	}

	// Should allow 100 tokens (burst capacity)
	if total != 100 {
		t.Fatalf("expected 100 total tokens allowed, got %d", total)
	}
}

func BenchmarkAllow(b *testing.B) {
	tb := New(100, 1000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tb.Allow()
	}
}

func BenchmarkAllowN(b *testing.B) {
	tb := New(1000, 1000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tb.AllowN(1)
	}
}
