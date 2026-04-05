package lru

import "sync"

// Node represents a doubly-linked list node for LRU cache
type Node struct {
	key   string
	value interface{}
	prev  *Node
	next  *Node
}

// Cache implements a thread-safe Least Recently Used cache
type Cache struct {
	capacity int
	cache    map[string]*Node
	head     *Node // dummy head node
	tail     *Node // dummy tail node
	mu       sync.RWMutex
}

// New creates a new LRU cache with given capacity
func New(capacity int) *Cache {
	head := &Node{}
	tail := &Node{}
	head.next = tail
	tail.prev = head

	return &Cache{
		capacity: capacity,
		cache:    make(map[string]*Node),
		head:     head,
		tail:     tail,
	}
}

// Get retrieves the value for a key and marks it as recently used
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node, exists := c.cache[key]
	if !exists {
		return nil, false
	}

	// Move node to the end (most recently used)
	c.moveToEnd(node)
	return node.value, true
}

// Put inserts or updates a key-value pair and marks it as recently used
func (c *Cache) Put(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update value and move to end
	if node, exists := c.cache[key]; exists {
		node.value = value
		c.moveToEnd(node)
		return
	}

	// Create new node and add to cache
	newNode := &Node{key: key, value: value}
	c.cache[key] = newNode
	c.addToEnd(newNode)

	// Evict LRU item if capacity exceeded
	if len(c.cache) > c.capacity {
		c.evictLRU()
	}
}

// moveToEnd moves a node to the end of the list (most recently used)
func (c *Cache) moveToEnd(node *Node) {
	c.removeNode(node)
	c.addToEnd(node)
}

// addToEnd adds a node to the end of the list (before tail)
func (c *Cache) addToEnd(node *Node) {
	node.prev = c.tail.prev
	node.next = c.tail
	c.tail.prev.next = node
	c.tail.prev = node
}

// removeNode removes a node from the list
func (c *Cache) removeNode(node *Node) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// evictLRU removes the least recently used item (first item after head)
func (c *Cache) evictLRU() {
	lruNode := c.head.next
	c.removeNode(lruNode)
	delete(c.cache, lruNode.key)
}

// Len returns the current number of items in the cache
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// Clear removes all items from the cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]*Node)
	c.head.next = c.tail
	c.tail.prev = c.head
}
