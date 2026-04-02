package internal

import "container/list"

// LRUCache is a thread-unsafe Least Recently Used cache with fixed capacity.
type LRUCache struct {
	capacity int
	cache    map[string]*list.Element // key -> list node
	list     *list.List               // doubly linked list for LRU order
}

// cacheEntry holds the data stored in a cache node.
type cacheEntry struct {
	key   string
	value interface{}
}

// NewLRUCache creates a new LRU cache with the given capacity.
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get retrieves the value for a key and marks it as recently used.
// Returns the value and true if found, nil and false otherwise.
func (lru *LRUCache) Get(key string) (interface{}, bool) {
	elem, ok := lru.cache[key]
	if !ok {
		return nil, false
	}

	// Move to front (mark as recently used)
	lru.list.MoveToFront(elem)
	return elem.Value.(*cacheEntry).value, true
}

// Put inserts or updates a key-value pair.
// If capacity is exceeded, evicts the least recently used item.
func (lru *LRUCache) Put(key string, value interface{}) {
	// Update existing key
	if elem, ok := lru.cache[key]; ok {
		elem.Value.(*cacheEntry).value = value
		lru.list.MoveToFront(elem)
		return
	}

	// Add new key
	entry := &cacheEntry{key: key, value: value}
	elem := lru.list.PushFront(entry)
	lru.cache[key] = elem

	// Evict LRU if over capacity
	if lru.list.Len() > lru.capacity {
		evicted := lru.list.Remove(lru.list.Back()).(*cacheEntry)
		delete(lru.cache, evicted.key)
	}
}

// Len returns the current number of items in the cache.
func (lru *LRUCache) Len() int {
	return lru.list.Len()
}

// Clear removes all items from the cache.
func (lru *LRUCache) Clear() {
	lru.cache = make(map[string]*list.Element)
	lru.list.Init()
}
