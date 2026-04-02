package ratelimit

import (
	"fmt"
	"time"
)

func ExampleTokenBucket() {
	// Create a rate limiter: 10 requests per second, burst of 5
	limiter := NewTokenBucket(10.0, 5)

	// Simulate requests
	for i := 1; i <= 12; i++ {
		if limiter.Allow() {
			fmt.Printf("Request %d: allowed\n", i)
		} else {
			fmt.Printf("Request %d: denied\n", i)
		}
	}

	fmt.Println("\nWaiting 200ms for tokens to refill...")
	time.Sleep(200 * time.Millisecond)

	// Now we have ~2 more tokens
	for i := 13; i <= 15; i++ {
		if limiter.Allow() {
			fmt.Printf("Request %d: allowed\n", i)
		} else {
			fmt.Printf("Request %d: denied\n", i)
		}
	}
}

func ExampleTokenBucket_AllowN() {
	// Create a rate limiter: 100 tokens per second, burst of 50
	limiter := NewTokenBucket(100.0, 50)

	// Request multiple tokens at once
	if limiter.AllowN(30) {
		fmt.Println("Batch of 30 tokens: allowed")
	}

	if limiter.AllowN(30) {
		fmt.Println("Batch of 30 tokens: allowed")
	} else {
		fmt.Println("Batch of 30 tokens: denied (only 20 left in burst)")
	}

	// Wait for refill
	time.Sleep(500 * time.Millisecond)

	if limiter.AllowN(40) {
		fmt.Println("Batch of 40 tokens after wait: allowed")
	}
}

func ExampleTokenBucket_RealWorld() {
	// Example: API rate limiter (1000 requests/min = 16.67 req/sec, burst of 50)
	limiter := NewTokenBucket(16.67, 50)

	fmt.Println("Simulating API requests with rate limiting:")

	requestCount := 0
	blockedCount := 0

	// Simulate 100 requests over time
	for i := 0; i < 100; i++ {
		if limiter.Allow() {
			requestCount++
			// Simulate some request processing
			if i%20 == 0 && i > 0 {
				fmt.Printf("  Processed %d requests\n", i)
				time.Sleep(60 * time.Millisecond) // Simulate request processing
			}
		} else {
			blockedCount++
		}
	}

	fmt.Printf("Total requests: %d\n", requestCount+blockedCount)
	fmt.Printf("Allowed: %d, Blocked: %d\n", requestCount, blockedCount)
}
