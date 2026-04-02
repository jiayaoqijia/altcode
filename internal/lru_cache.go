package internal

import "sync"

// Node represents a node in the doubly linked list
type Node struct {
	key  string
	val  interface{}
	prev *Node
	next *Node
}

// LRUCache implements a thread-safe Least Recently Used cache
type LRUCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*Node
	head     *Node // sentinel node at the front
	tail     *Node // sentinel node at the back
}

// NewLRUCache creates a new LRU cache with the given capacity
func NewLRUCache(capacity int) *LRUCache {
	head := &Node{}
	tail := &Node{}
	head.next = tail
	tail.prev = head

	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*Node),
		head:     head,
		tail:     tail,
	}
}

// Get retrieves the value for a key, moving it to the most recently used position
func (lru *LRUCache) Get(key string) (interface{}, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	node, exists := lru.cache[key]
	if !exists {
		return nil, false
	}

	// Move node to the front (most recently used)
	lru.moveToFront(node)
	return node.val, true
}

// Put inserts or updates a key-value pair, moving it to the most recently used position
// If capacity is exceeded, the least recently used item is evicted
func (lru *LRUCache) Put(key string, value interface{}) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	// If key exists, update its value and move to front
	if node, exists := lru.cache[key]; exists {
		node.val = value
		lru.moveToFront(node)
		return
	}

	// Create new node and add to front
	newNode := &Node{key: key, val: value}
	lru.cache[key] = newNode
	lru.addToFront(newNode)

	// Evict LRU item if capacity exceeded
	if len(lru.cache) > lru.capacity {
		lru.evictLRU()
	}
}

// moveToFront moves an existing node to the front of the list
func (lru *LRUCache) moveToFront(node *Node) {
	lru.removeNode(node)
	lru.addToFront(node)
}

// addToFront adds a node right after the head sentinel
func (lru *LRUCache) addToFront(node *Node) {
	node.prev = lru.head
	node.next = lru.head.next
	lru.head.next.prev = node
	lru.head.next = node
}

// removeNode removes a node from the doubly linked list
func (lru *LRUCache) removeNode(node *Node) {
	node.prev.next = node.next
	node.next.prev = node.prev
}

// evictLRU removes the least recently used item (node before tail sentinel)
func (lru *LRUCache) evictLRU() {
	lruNode := lru.tail.prev
	lru.removeNode(lruNode)
	delete(lru.cache, lruNode.key)
}

// Size returns the current number of items in the cache
func (lru *LRUCache) Size() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return len(lru.cache)
}

// Clear removes all items from the cache
func (lru *LRUCache) Clear() {
	lru.mu.Lock()
	defer lru.mu.Unlock()
	lru.cache = make(map[string]*Node)
	lru.head.next = lru.tail
	lru.tail.prev = lru.head
}
