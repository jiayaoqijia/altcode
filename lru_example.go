package main

import (
	"container/list"
	"fmt"
	"sync"
)

// LRUCache is a thread-safe Least Recently Used cache.
type LRUCache struct {
	capacity int
	cache    map[interface{}]*list.Element
	list     *list.List
	mu       sync.RWMutex
}

// entry represents a key-value pair stored in the cache.
type entry struct {
	key   interface{}
	value interface{}
}

// NewLRUCache creates a new LRU cache with the given capacity.
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[interface{}]*list.Element),
		list:     list.New(),
	}
}

// Get retrieves a value by key, returning (value, found).
// Accessing a key marks it as recently used.
func (lru *LRUCache) Get(key interface{}) (interface{}, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	elem, found := lru.cache[key]
	if !found {
		return nil, false
	}

	// Move to front (most recently used)
	lru.list.MoveToFront(elem)
	return elem.Value.(*entry).value, true
}

// Put adds or updates a key-value pair.
// If capacity is exceeded, the least recently used item is evicted.
func (lru *LRUCache) Put(key interface{}, value interface{}) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	// If key exists, update it and move to front
	if elem, found := lru.cache[key]; found {
		lru.list.MoveToFront(elem)
		elem.Value.(*entry).value = value
		return
	}

	// Add new entry to front
	elem := lru.list.PushFront(&entry{key: key, value: value})
	lru.cache[key] = elem

	// Evict LRU if capacity exceeded
	if lru.list.Len() > lru.capacity {
		back := lru.list.Back()
		lru.list.Remove(back)
		delete(lru.cache, back.Value.(*entry).key)
	}
}

// Len returns the current number of items in the cache.
func (lru *LRUCache) Len() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return lru.list.Len()
}

// Clear removes all items from the cache.
func (lru *LRUCache) Clear() {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	lru.cache = make(map[interface{}]*list.Element)
	lru.list.Init()
}

func main() {
	// Create an LRU cache with capacity of 3
	cache := NewLRUCache(3)

	fmt.Println("=== LRU Cache Example ===\n")

	// Add some items
	fmt.Println("Adding items: a=1, b=2, c=3")
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3)
	fmt.Printf("Cache size: %d\n\n", cache.Len())

	// Access item "a" to mark it as recently used
	fmt.Println("Accessing 'a'")
	val, _ := cache.Get("a")
	fmt.Printf("Got a = %v\n\n", val)

	// Add item "d" - should evict "b" (least recently used)
	fmt.Println("Adding d=4 (should evict 'b')")
	cache.Put("d", 4)
	fmt.Printf("Cache size: %d\n", cache.Len())

	// Try to get "b" - should not be found
	val, found := cache.Get("b")
	fmt.Printf("Get b: found=%v, value=%v\n\n", found, val)

	// Check what's still in cache
	fmt.Println("Checking remaining items:")
	for _, key := range []string{"a", "c", "d"} {
		val, found := cache.Get(key)
		if found {
			fmt.Printf("  %s = %v\n", key, val)
		}
	}

	fmt.Printf("\nFinal cache size: %d\n", cache.Len())

	// Update test
	fmt.Println("\n=== Update Test ===")
	cache.Clear()
	cache.Put("x", 10)
	cache.Put("y", 20)
	fmt.Printf("Before update: x = %v\n", cache.Get("x"))
	
	cache.Put("x", 100) // Update existing key
	fmt.Printf("After update: x = %v\n", cache.Get("x"))
	fmt.Printf("Cache size (should be 2): %d\n", cache.Len())
}
