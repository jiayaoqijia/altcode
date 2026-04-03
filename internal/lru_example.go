package internal

import "fmt"

// ExampleLRUUsage demonstrates how to use the LRU cache.
func ExampleLRUUsage() {
	// Create an LRU cache with capacity of 3
	cache := NewLRUCache(3)

	// Put some values
	cache.Put("user:1", "Alice")
	cache.Put("user:2", "Bob")
	cache.Put("user:3", "Charlie")

	fmt.Printf("Cache size: %d\n", cache.Len()) // Output: 3

	// Get a value
	if val, ok := cache.Get("user:1"); ok {
		fmt.Printf("user:1 = %v\n", val) // Output: Alice
	}

	// Add another value (evicts least recently used)
	cache.Put("user:4", "David")

	fmt.Printf("Cache size after adding 4th item: %d\n", cache.Len()) // Output: 3

	// Check if user:2 was evicted (it was, because we didn't access it)
	if _, ok := cache.Get("user:2"); !ok {
		fmt.Println("user:2 was evicted") // Output: user:2 was evicted
	}

	// Check if user:1 still exists (it does, because we accessed it recently)
	if val, ok := cache.Get("user:1"); ok {
		fmt.Printf("user:1 still exists: %v\n", val) // Output: user:1 still exists: Alice
	}

	// Update an existing key
	cache.Put("user:3", "Charlie Updated")
	if val, ok := cache.Get("user:3"); ok {
		fmt.Printf("user:3 updated: %v\n", val) // Output: user:3 updated: Charlie Updated
	}
}
