package daemon

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiter_Update(t *testing.T) {
	rl := &RateLimiter{}
	resetAt := time.Now().Add(5 * time.Minute)
	rl.Update(500, resetAt)

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.remaining != 500 {
		t.Errorf("remaining = %d, want 500", rl.remaining)
	}
	if !rl.resetAt.Equal(resetAt) {
		t.Errorf("resetAt = %v, want %v", rl.resetAt, resetAt)
	}
}

func TestRateLimiter_ShouldThrottle(t *testing.T) {
	rl := &RateLimiter{}
	rl.Update(50, time.Now().Add(time.Minute))

	if !rl.ShouldThrottle() {
		t.Error("ShouldThrottle = false, want true (remaining=50)")
	}
}

func TestRateLimiter_NotThrottled(t *testing.T) {
	rl := &RateLimiter{}
	rl.Update(500, time.Now().Add(time.Minute))

	if rl.ShouldThrottle() {
		t.Error("ShouldThrottle = true, want false (remaining=500)")
	}
}

func TestRateLimiter_ShouldThrottle_Boundary(t *testing.T) {
	rl := &RateLimiter{}

	// Exactly 100 is not throttled.
	rl.Update(100, time.Now().Add(time.Minute))
	if rl.ShouldThrottle() {
		t.Error("remaining=100 should not throttle")
	}

	// 99 is throttled.
	rl.Update(99, time.Now().Add(time.Minute))
	if !rl.ShouldThrottle() {
		t.Error("remaining=99 should throttle")
	}
}

func TestRateLimiter_WaitForReset_AlreadyPast(t *testing.T) {
	rl := &RateLimiter{}
	rl.Update(0, time.Now().Add(-time.Second))

	err := rl.WaitForReset(context.Background())
	if err != nil {
		t.Fatalf("WaitForReset: %v", err)
	}
}

func TestRateLimiter_WaitForReset_ContextCancel(t *testing.T) {
	rl := &RateLimiter{}
	rl.Update(0, time.Now().Add(time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := rl.WaitForReset(ctx)
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestRetryWithBackoff_Success(t *testing.T) {
	var calls int32
	err := RetryWithBackoff(context.Background(), 3, func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err != nil {
		t.Fatalf("RetryWithBackoff: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestRetryWithBackoff_TransientThenSuccess(t *testing.T) {
	var calls int32
	err := RetryWithBackoff(context.Background(), 3, func() error {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			return fmt.Errorf("502 Bad Gateway")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RetryWithBackoff: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestRetryWithBackoff_PermanentError(t *testing.T) {
	var calls int32
	err := RetryWithBackoff(context.Background(), 3, func() error {
		atomic.AddInt32(&calls, 1)
		return fmt.Errorf("404 Not Found")
	})
	if err == nil {
		t.Fatal("expected error for permanent failure")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %q, want 404", err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", n)
	}
}

func TestRetryWithBackoff_Exhausted(t *testing.T) {
	var calls int32
	err := RetryWithBackoff(context.Background(), 2, func() error {
		atomic.AddInt32(&calls, 1)
		return fmt.Errorf("503 Service Unavailable")
	})
	if err == nil {
		t.Fatal("expected error when retries exhausted")
	}
	if !strings.Contains(err.Error(), "max retries") {
		t.Errorf("err = %q, want max retries", err)
	}
	// maxRetries=2 means 3 total attempts (0, 1, 2).
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestRetryWithBackoff_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := RetryWithBackoff(ctx, 5, func() error {
		return fmt.Errorf("503 unavailable")
	})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestVerifyWebhookSignature_Valid(t *testing.T) {
	secret := "test-secret-key"
	payload := []byte(`{"action":"opened","number":1}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifyWebhookSignature(payload, sig, secret) {
		t.Error("valid signature rejected")
	}
}

func TestVerifyWebhookSignature_Invalid(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	sig := "sha256=0000000000000000000000000000000000000000000000000000000000000000"

	if VerifyWebhookSignature(payload, sig, "real-secret") {
		t.Error("invalid signature accepted")
	}
}

func TestVerifyWebhookSignature_Empty(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	sig := "sha256=abc123"

	if VerifyWebhookSignature(payload, sig, "") {
		t.Error("empty secret should always return false")
	}
}

func TestIsTransient(t *testing.T) {
	cases := []struct {
		err    string
		expect bool
	}{
		{"502 Bad Gateway", true},
		{"503 Service Unavailable", true},
		{"connection refused", true},
		{"request timeout", true},
		{"404 Not Found", false},
		{"401 Unauthorized", false},
		{"permission denied", false},
	}
	for _, tc := range cases {
		got := isTransient(fmt.Errorf("%s", tc.err))
		if got != tc.expect {
			t.Errorf(
				"isTransient(%q) = %v, want %v",
				tc.err, got, tc.expect,
			)
		}
	}
}
