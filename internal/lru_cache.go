package internal

import (
	"container/list"
	"sync"
)

// LRUCache is a thread-safe Least Recently Used cache with fixed capacity.
type LRUCache struct {
	capacity int
	cache    map[interface{}]*list.Element
	list     *list.List
	mu       sync.Mutex
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

// Get retrieves the value associated with the given key.
// If the key exists, it marks the entry as recently used and returns the value.
// If the key doesn't exist, it returns nil and false.
func (lru *LRUCache) Get(key interface{}) (interface{}, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	elem, ok := lru.cache[key]
	if !ok {
		return nil, false
	}

	// Move to front (most recently used)
	lru.list.MoveToFront(elem)
	return elem.Value.(*entry).value, true
}

// Put inserts or updates a key-value pair in the cache.
// If the key already exists, it updates the value and marks it as recently used.
// If the cache is at capacity, it evicts the least recently used item.
func (lru *LRUCache) Put(key, value interface{}) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	// If key exists, update and move to front
	if elem, ok := lru.cache[key]; ok {
		elem.Value.(*entry).value = value
		lru.list.MoveToFront(elem)
		return
	}

	// Add new entry to front
	ent := &entry{key: key, value: value}
	elem := lru.list.PushFront(ent)
	lru.cache[key] = elem

	// Evict LRU if over capacity
	if lru.list.Len() > lru.capacity {
		lruElem := lru.list.Back()
		lru.list.Remove(lruElem)
		delete(lru.cache, lruElem.Value.(*entry).key)
	}
}

// Len returns the current number of items in the cache.
func (lru *LRUCache) Len() int {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	return lru.list.Len()
}
