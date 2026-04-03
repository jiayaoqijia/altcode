package lru

import (
	"container/list"
	"sync"
)

// Cache is a thread-safe LRU cache implementation.
type Cache struct {
	capacity int
	mu       sync.RWMutex
	items    map[string]*list.Element
	list     *list.List
}

// entry represents a single cache entry.
type entry struct {
	key   string
	value interface{}
}

// New creates a new LRU cache with the given capacity.
func New(capacity int) *Cache {
	if capacity <= 0 {
		capacity = 1
	}
	return &Cache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get retrieves a value from the cache by key and marks it as recently used.
// Returns the value and true if found, nil and false otherwise.
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		return nil, false
	}

	// Move to front (most recently used)
	c.list.MoveToFront(elem)
	return elem.Value.(*entry).value, true
}

// Put inserts or updates a key-value pair in the cache.
// If the cache is at capacity, the least recently used item is evicted.
func (c *Cache) Put(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key exists, update it
	if elem, exists := c.items[key]; exists {
		elem.Value.(*entry).value = value
		c.list.MoveToFront(elem)
		return
	}

	// Add new entry to front
	elem := c.list.PushFront(&entry{key: key, value: value})
	c.items[key] = elem

	// Evict LRU if at capacity
	if c.list.Len() > c.capacity {
		evicted := c.list.Remove(c.list.Back()).(*entry)
		delete(c.items, evicted.key)
	}
}

// Len returns the number of items currently in the cache.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.list.Len()
}

// Clear removes all items from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.list = list.New()
}
