package lru

// Cache is an LRU (Least Recently Used) cache with O(1) Get and Put operations.
type Cache struct {
	capacity int
	cache    map[int]*listNode
	head     *listNode // most recently used
	tail     *listNode // least recently used
}

type listNode struct {
	key   int
	value int
	prev  *listNode
	next  *listNode
}

// New creates a new LRU cache with the given capacity.
func New(capacity int) *Cache {
	return &Cache{
		capacity: capacity,
		cache:    make(map[int]*listNode),
	}
}

// Get returns the value for key if it exists, otherwise -1.
// Moves the key to the front (most recently used).
func (c *Cache) Get(key int) int {
	if node, ok := c.cache[key]; ok {
		c.moveToFront(node)
		return node.value
	}
	return -1
}

// Put adds a key-value pair to the cache.
// If the cache is at capacity, evicts the least recently used item.
func (c *Cache) Put(key int, value int) {
	if node, ok := c.cache[key]; ok {
		node.value = value
		c.moveToFront(node)
		return
	}

	// Evict LRU if at capacity
	if len(c.cache) >= c.capacity {
		c.evict()
	}

	// Add new node
	node := &listNode{key: key, value: value}
	c.cache[key] = node
	c.addToFront(node)
}

func (c *Cache) moveToFront(node *listNode) {
	if c.head == node {
		return // Already at front
	}
	c.removeNode(node)
	c.addToFront(node)
}

func (c *Cache) addToFront(node *listNode) {
	node.prev = nil
	node.next = c.head

	if c.head != nil {
		c.head.prev = node
	}
	c.head = node

	if c.tail == nil {
		c.tail = node
	}
}

func (c *Cache) removeNode(node *listNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}

	if node.next != nil {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
}

func (c *Cache) evict() {
	if c.tail == nil {
		return
	}
	delete(c.cache, c.tail.key)
	c.removeNode(c.tail)
}
