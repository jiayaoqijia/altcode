package internal

type node struct {
	key   string
	value string
	prev  *node
	next  *node
}

type LRUCache struct {
	capacity int
	cache    map[string]*node
	head     *node
	tail     *node
}

func NewLRUCache(capacity int) *LRUCache {
	cache := &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*node),
	}
	// Initialize dummy head and tail nodes
	cache.head = &node{}
	cache.tail = &node{}
	cache.head.next = cache.tail
	cache.tail.prev = cache.head
	return cache
}

func (c *LRUCache) Get(key string) (string, bool) {
	if n, ok := c.cache[key]; ok {
		c.moveToFront(n)
		return n.value, true
	}
	return "", false
}

func (c *LRUCache) Put(key string, value string) {
	if n, ok := c.cache[key]; ok {
		n.value = value
		c.moveToFront(n)
		return
	}

	if len(c.cache) >= c.capacity {
		c.evict()
	}

	newNode := &node{
		key:   key,
		value: value,
	}
	c.cache[key] = newNode
	c.addToFront(newNode)
}

func (c *LRUCache) moveToFront(n *node) {
	c.remove(n)
	c.addToFront(n)
}

func (c *LRUCache) addToFront(n *node) {
	n.prev = c.head
	n.next = c.head.next
	c.head.next.prev = n
	c.head.next = n
}

func (c *LRUCache) remove(n *node) {
	n.prev.next = n.next
	n.next.prev = n.prev
	n.prev = nil
	n.next = nil
}

func (c *LRUCache) evict() {
	last := c.tail.prev
	if last == c.head {
		return
	}
	c.remove(last)
	delete(c.cache, last.key)
}
