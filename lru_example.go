package main

import (
	"fmt"
)

func main() {
	// Create an LRU cache with capacity 3
	cache := NewLRUCache(3)

	// Put some values
	fmt.Println("=== LRU Cache Example ===\n")

	fmt.Println("1. Adding 3 items (capacity=3):")
	cache.Put("user:1", "Alice")
	fmt.Printf("   Put(user:1, Alice) -> Size: %d\n", cache.Size())

	cache.Put("user:2", "Bob")
	fmt.Printf("   Put(user:2, Bob) -> Size: %d\n", cache.Size())

	cache.Put("user:3", "Charlie")
	fmt.Printf("   Put(user:3, Charlie) -> Size: %d\n", cache.Size())

	// Get a value (moves it to front)
	fmt.Println("\n2. Getting user:1 (marks as recently used):")
	val, ok := cache.Get("user:1")
	fmt.Printf("   Get(user:1) -> %v (found: %v)\n", val, ok)

	// Add a 4th item - should evict LRU (user:2)
	fmt.Println("\n3. Adding 4th item (should evict LRU):")
	cache.Put("user:4", "Diana")
	fmt.Printf("   Put(user:4, Diana) -> Size: %d\n", cache.Size())

	// Try to get evicted item
	fmt.Println("\n4. Trying to get evicted item (user:2):")
	val, ok = cache.Get("user:2")
	fmt.Printf("   Get(user:2) -> %v (found: %v)\n", val, ok)

	// Get remaining items
	fmt.Println("\n5. Getting remaining items:")
	val, ok = cache.Get("user:1")
	fmt.Printf("   Get(user:1) -> %v (found: %v)\n", val, ok)

	val, ok = cache.Get("user:3")
	fmt.Printf("   Get(user:3) -> %v (found: %v)\n", val, ok)

	val, ok = cache.Get("user:4")
	fmt.Printf("   Get(user:4) -> %v (found: %v)\n", val, ok)

	// Update existing key
	fmt.Println("\n6. Updating existing key:")
	cache.Put("user:1", "Alice Updated")
	val, ok = cache.Get("user:1")
	fmt.Printf("   Put(user:1, Alice Updated) -> Get returns: %v\n", val)

	// Clear cache
	fmt.Println("\n7. Clearing cache:")
	cache.Clear()
	fmt.Printf("   After Clear() -> Size: %d\n", cache.Size())

	// Concurrent access example
	fmt.Println("\n8. Concurrent access (thread-safe):")
	cache = NewLRUCache(2)
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 5; i++ {
			cache.Put(fmt.Sprintf("key%d", i), i*10)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 5; i++ {
			cache.Get(fmt.Sprintf("key%d", i))
		}
		done <- true
	}()

	<-done
	<-done
	fmt.Printf("   After concurrent ops -> Size: %d\n", cache.Size())
}
