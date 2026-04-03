package ratelimit

import (
	"fmt"
	"time"
)

// ExampleTokenBucket demonstrates basic usage of the token bucket rate limiter.
func ExampleTokenBucket() {
	// Create a rate limiter: 5 requests per second with burst of 10
	limiter := NewTokenBucket(5.0, 10)

	// Simulate handling requests
	for i := 0; i < 15; i++ {
		if limiter.Allow() {
			fmt.Printf("Request %d: allowed\n", i+1)
		} else {
			fmt.Printf("Request %d: denied\n", i+1)
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
	// Request 11: denied
	// Request 12: denied
	// Request 13: denied
	// Request 14: denied
	// Request 15: denied
}

// ExampleTokenBucket_AllowN demonstrates consuming multiple tokens at once.
func ExampleTokenBucket_AllowN() {
	limiter := NewTokenBucket(5.0, 20)

	// Try different batch sizes
	fmt.Println("Batch of 5:", limiter.AllowN(5))
	fmt.Println("Batch of 10:", limiter.AllowN(10))
	fmt.Println("Batch of 6:", limiter.AllowN(6))  // Only 5 left, should fail
	fmt.Println("Batch of 5:", limiter.AllowN(5))  // Exactly the remaining amount
	fmt.Println("Batch of 1:", limiter.AllowN(1))  // No tokens left

	// Output:
	// Batch of 5: true
	// Batch of 10: true
	// Batch of 6: false
	// Batch of 5: true
	// Batch of 1: false
}

// ExampleTokenBucket_Reserve demonstrates reserving tokens with wait time.
func ExampleTokenBucket_Reserve() {
	limiter := NewTokenBucket(2.0, 2) // 2 tokens/sec, burst of 2

	start := time.Now()

	// First request uses burst
	wait1 := limiter.Reserve(1)
	fmt.Printf("Reserve 1 token: wait %v\n", wait1)

	// Second request uses remaining burst
	wait2 := limiter.Reserve(1)
	fmt.Printf("Reserve 1 token: wait %v\n", wait2)

	// Third request requires waiting for new tokens
	wait3 := limiter.Reserve(1)
	fmt.Printf("Reserve 1 token: wait %v\n", wait3)

	// Actually wait if needed
	if wait3 > 0 {
		time.Sleep(wait3)
	}

	elapsed := time.Since(start)
	fmt.Printf("Total elapsed: %v\n", elapsed)
}

// ExampleTokenBucket_RateLimiting demonstrates rate limiting HTTP-like requests.
func ExampleTokenBucket_RateLimiting() {
	// Allow 100 requests per second with burst of 200
	limiter := NewTokenBucket(100.0, 200)

	// Simulate incoming requests
	allowed := 0
	denied := 0

	for i := 0; i < 300; i++ {
		if limiter.Allow() {
			allowed++
		} else {
			denied++
		}
	}

	fmt.Printf("Allowed: %d, Denied: %d\n", allowed, denied)
	// Output:
	// Allowed: 200, Denied: 100
}

// ExampleTokenBucket_Refill demonstrates token refilling over time.
func ExampleTokenBucket_Refill() {
	limiter := NewTokenBucket(2.0, 5) // 2 tokens per second, burst of 5

	// Use all burst tokens
	for i := 0; i < 5; i++ {
		limiter.Allow()
	}
	fmt.Printf("After burst: %d denied\n", countDenied(limiter, 3))

	// Wait and check refill
	time.Sleep(1500 * time.Millisecond)
	fmt.Printf("After 1.5 seconds: %d allowed\n", countAllowed(limiter, 3))

	// Output:
	// After burst: 3 denied
	// After 1.5 seconds: 3 allowed
}

func countAllowed(tb *TokenBucket, n int) int {
	count := 0
	for i := 0; i < n; i++ {
		if tb.Allow() {
			count++
		}
	}
	return count
}

func countDenied(tb *TokenBucket, n int) int {
	count := 0
	for i := 0; i < n; i++ {
		if !tb.Allow() {
			count++
		}
	}
	return count
}
