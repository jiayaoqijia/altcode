package lru

import "container/list"

// Cache is a thread-unsafe Least Recently Used cache with fixed capacity.
type Cache struct {
	capacity int
	cache    map[string]*list.Element // key -> list node
	list     *list.List               // doubly linked list for LRU order
}

// entry holds the data stored in a cache node.
type entry struct {
	key   string
	value interface{}
}

// New creates a new LRU cache with the given capacity.
func New(capacity int) *Cache {
	return &Cache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get retrieves the value for a key and marks it as recently used.
// Returns the value and true if found, nil and false otherwise.
func (c *Cache) Get(key string) (interface{}, bool) {
	elem, ok := c.cache[key]
	if !ok {
		return nil, false
	}

	// Move to front (mark as recently used)
	c.list.MoveToFront(elem)
	return elem.Value.(*entry).value, true
}

// Put inserts or updates a key-value pair.
// If capacity is exceeded, evicts the least recently used item.
func (c *Cache) Put(key string, value interface{}) {
	// Update existing key
	if elem, ok := c.cache[key]; ok {
		elem.Value.(*entry).value = value
		c.list.MoveToFront(elem)
		return
	}

	// Add new key
	e := &entry{key: key, value: value}
	elem := c.list.PushFront(e)
	c.cache[key] = elem

	// Evict LRU if over capacity
	if c.list.Len() > c.capacity {
		evicted := c.list.Remove(c.list.Back()).(*entry)
		delete(c.cache, evicted.key)
	}
}

// Len returns the current number of items in the cache.
func (c *Cache) Len() int {
	return c.list.Len()
}

// Clear removes all items from the cache.
func (c *Cache) Clear() {
	c.cache = make(map[string]*list.Element)
	c.list.Init()
}
