package lru

// Node represents an element in the doubly linked list
type Node[K comparable, V any] struct {
	Key   K
	Value V
	Next  *Node[K, V]
	Prev  *Node[K, V]
}

// LRUCache is a Least Recently Used cache implementation
type LRUCache[K comparable, V any] struct {
	capacity int
	cache    map[K]*Node[K, V]
	head     *Node[K, V]
	tail     *Node[K, V]
}

// New creates a new LRU cache with the given capacity
func New[K comparable, V any](capacity int) *LRUCache[K, V] {
	cache := &LRUCache[K, V]{
		capacity: capacity,
		cache:    make(map[K]*Node[K, V]),
	}

	// Initialize sentinel nodes
	cache.head = &Node[K, V]{}
	cache.tail = &Node[K, V]{}
	cache.head.Next = cache.tail
	cache.tail.Prev = cache.head

	return cache
}

// Get retrieves a value from the cache by key
// Returns the value and true if found, zero value and false if not found
func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	node, exists := c.cache[key]
	if !exists {
		var zero V
		return zero, false
	}

	// Move to head (most recently used)
	c.moveToHead(node)
	return node.Value, true
}

// Put adds or updates a key-value pair in the cache
func (c *LRUCache[K, V]) Put(key K, value V) {
	if node, exists := c.cache[key]; exists {
		// Update existing node
		node.Value = value
		c.moveToHead(node)
		return
	}

	// Create new node
	newNode := &Node[K, V]{
		Key:   key,
		Value: value,
	}

	// Add to cache and move to head
	c.cache[key] = newNode
	c.addToHead(newNode)

	// Evict if over capacity
	if len(c.cache) > c.capacity {
		c.removeLRU()
	}
}

// moveToHead moves a node to right after the head (most recently used)
func (c *LRUCache[K, V]) moveToHead(node *Node[K, V]) {
	c.removeNode(node)
	c.addToHead(node)
}

// addToHead adds a node right after the head
func (c *LRUCache[K, V]) addToHead(node *Node[K, V]) {
	node.Prev = c.head
	node.Next = c.head.Next
	c.head.Next.Prev = node
	c.head.Next = node
}

// removeNode removes a node from the doubly linked list
func (c *LRUCache[K, V]) removeNode(node *Node[K, V]) {
	node.Prev.Next = node.Next
	node.Next.Prev = node.Prev
}

// removeLRU removes the least recently used item (right before tail)
func (c *LRUCache[K, V]) removeLRU() {
	lru := c.tail.Prev
	c.removeNode(lru)
	delete(c.cache, lru.Key)
}

// Len returns the current number of items in the cache
func (c *LRUCache[K, V]) Len() int {
	return len(c.cache)
}
