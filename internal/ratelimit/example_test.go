package ratelimit

import (
	"fmt"
	"time"
)

// ExampleTokenBucket demonstrates basic usage of the token bucket rate limiter.
func ExampleTokenBucket_basic() {
	// Create a rate limiter: 5 requests per second, with burst of 10
	limiter := New(10, 5)

	// Simulate 15 requests
	for i := 1; i <= 15; i++ {
		if limiter.Allow() {
			fmt.Printf("Request %d: allowed\n", i)
		} else {
			fmt.Printf("Request %d: rate limited\n", i)
		}
	}

	// Wait for tokens to refill
	time.Sleep(500 * time.Millisecond)

	fmt.Println("\nAfter 500ms refill:")

	// Try again
	for i := 16; i <= 18; i++ {
		if limiter.Allow() {
			fmt.Printf("Request %d: allowed\n", i)
		} else {
			fmt.Printf("Request %d: rate limited\n", i)
		}
	}

	// Output:
	// Request 1: allowed
	// Request 2: allowed
	// Request 3: allowed
	// Request 4: allowed
	// Request 5: allowed
	// Request 6: allowed
	// Request 7: allowed
	// Request 8: allowed
	// Request 9: allowed
	// Request 10: allowed
	// Request 11: rate limited
	// Request 12: rate limited
	// Request 13: rate limited
	// Request 14: rate limited
	// Request 15: rate limited
	//
	// After 500ms refill:
	// Request 16: allowed
	// Request 17: allowed
	// Request 18: rate limited
}

// ExampleTokenBucket_multiToken demonstrates allowing multiple tokens at once.
func ExampleTokenBucket_multiToken() {
	// Create a rate limiter: 100 tokens per second, burst of 100
	limiter := New(100, 100)

	// Batch operation needing 30 tokens
	if limiter.AllowN(30) {
		fmt.Println("Batch operation 1 allowed")
	}

	// Another batch operation needing 50 tokens
	if limiter.AllowN(50) {
		fmt.Println("Batch operation 2 allowed")
	}

	// Third batch operation needing 30 tokens (only 20 left)
	if limiter.AllowN(30) {
		fmt.Println("Batch operation 3 allowed")
	} else {
		fmt.Println("Batch operation 3 denied (need 30, have", limiter.Tokens(), ")")
	}

	// Output:
	// Batch operation 1 allowed
	// Batch operation 2 allowed
	// Batch operation 3 denied (need 30, have 20)
}

// ExampleTokenBucket_reset demonstrates resetting the bucket.
func ExampleTokenBucket_reset() {
	limiter := New(5, 1)

	// Exhaust tokens
	for limiter.Allow() {
	}

	fmt.Printf("Tokens after exhaustion: %.0f\n", limiter.Tokens())

	// Reset to full
	limiter.Reset()

	fmt.Printf("Tokens after reset: %.0f\n", limiter.Tokens())

	// Output:
	// Tokens after exhaustion: 0
	// Tokens after reset: 5
}
