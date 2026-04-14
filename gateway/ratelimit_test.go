package gateway

import (
	"testing"
)

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxAttempts:    3,
		WindowSeconds:  60,
		LockoutSeconds: 30,
	})
	defer rl.Stop()

	for i := 0; i < 3; i++ {
		if !rl.Allow("test", "1.2.3.4") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxAttempts:    2,
		WindowSeconds:  60,
		LockoutSeconds: 30,
	})
	defer rl.Stop()

	rl.Allow("test", "1.2.3.4")
	rl.Allow("test", "1.2.3.4")

	if rl.Allow("test", "1.2.3.4") {
		t.Fatal("3rd request should be blocked")
	}
}

func TestRateLimiter_AllowsLoopback(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxAttempts:    1,
		WindowSeconds:  60,
		LockoutSeconds: 30,
	})
	defer rl.Stop()

	// Exhaust limit from loopback
	for i := 0; i < 10; i++ {
		if !rl.Allow("test", "127.0.0.1") {
			t.Fatal("loopback should always be allowed")
		}
	}
}

func TestRateLimiter_SeparateScopes(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxAttempts:    1,
		WindowSeconds:  60,
		LockoutSeconds: 30,
	})
	defer rl.Stop()

	rl.Allow("scope1", "1.2.3.4")
	if rl.Allow("scope1", "1.2.3.4") {
		t.Fatal("should be blocked in scope1")
	}
	if !rl.Allow("scope2", "1.2.3.4") {
		t.Fatal("scope2 should be independent")
	}
}

func TestRateLimiter_StopIdempotent(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		MaxAttempts:    5,
		WindowSeconds:  60,
		LockoutSeconds: 30,
	})
	rl.Stop()
	rl.Stop() // should not panic
}
