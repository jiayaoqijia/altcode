package internal

import "fmt"

// ExampleLRUCache demonstrates the LRU cache functionality
func ExampleLRUCache() {
	// Create a cache with capacity 3
	cache := NewLRUCache(3)

	// Add items
	cache.Put("user:1", "Alice")
	cache.Put("user:2", "Bob")
	cache.Put("user:3", "Charlie")

	fmt.Println("Cache size:", cache.Size())

	// Get an item (moves it to most recent)
	val, _ := cache.Get("user:1")
	fmt.Println("Got user:1:", val)

	// Add another item, should evict user:2 (least recent)
	cache.Put("user:4", "David")

	// Check what's in the cache
	if _, ok := cache.Get("user:2"); ok {
		fmt.Println("user:2 still in cache")
	} else {
		fmt.Println("user:2 was evicted (LRU)")
	}

	fmt.Println("Final cache size:", cache.Size())
}
