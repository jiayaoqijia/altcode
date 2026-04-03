package internal

import (
	"testing"
)

func TestLRUCachePut(t *testing.T) {
	cache := NewLRUCache(2)
	cache.Put("a", 1)
	cache.Put("b", 2)

	if cache.Len() != 2 {
		t.Errorf("expected len 2, got %d", cache.Len())
	}
}

func TestLRUCacheGet(t *testing.T) {
	cache := NewLRUCache(2)
	cache.Put("a", 1)
	cache.Put("b", 2)

	val, ok := cache.Get("a")
	if !ok || val != 1 {
		t.Errorf("expected 1, got %v", val)
	}

	val, ok = cache.Get("nonexistent")
	if ok {
		t.Error("expected key not found")
	}
}

func TestLRUCacheEviction(t *testing.T) {
	cache := NewLRUCache(2)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3) // Should evict "a"

	if cache.Len() != 2 {
		t.Errorf("expected len 2 after eviction, got %d", cache.Len())
	}

	_, ok := cache.Get("a")
	if ok {
		t.Error("expected 'a' to be evicted")
	}

	val, ok := cache.Get("b")
	if !ok || val != 2 {
		t.Errorf("expected 'b' to exist with value 2, got %v", val)
	}

	val, ok = cache.Get("c")
	if !ok || val != 3 {
		t.Errorf("expected 'c' to exist with value 3, got %v", val)
	}
}

func TestLRUCacheRecentlyUsed(t *testing.T) {
	cache := NewLRUCache(3)
	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3)

	// Access "a" to make it recently used
	cache.Get("a")

	// Add "d", should evict "b" (least recently used)
	cache.Put("d", 4)

	if cache.Len() != 3 {
		t.Errorf("expected len 3, got %d", cache.Len())
	}

	_, ok := cache.Get("b")
	if ok {
		t.Error("expected 'b' to be evicted")
	}

	val, ok := cache.Get("a")
	if !ok || val != 1 {
		t.Errorf("expected 'a' to exist, got %v", val)
	}
}

func TestLRUCacheUpdate(t *testing.T) {
	cache := NewLRUCache(2)
	cache.Put("a", 1)
	cache.Put("b", 2)

	// Update "a" with new value
	cache.Put("a", 10)

	val, ok := cache.Get("a")
	if !ok || val != 10 {
		t.Errorf("expected 10, got %v", val)
	}

	// "a" should be most recently used now
	// So adding "c" should evict "b"
	cache.Put("c", 3)

	_, ok = cache.Get("b")
	if ok {
		t.Error("expected 'b' to be evicted after update")
	}
}

func TestLRUCacheCapacityOne(t *testing.T) {
	cache := NewLRUCache(1)
	cache.Put("a", 1)
	cache.Put("b", 2)

	if cache.Len() != 1 {
		t.Errorf("expected len 1, got %d", cache.Len())
	}

	_, ok := cache.Get("a")
	if ok {
		t.Error("expected 'a' to be evicted")
	}

	val, ok := cache.Get("b")
	if !ok || val != 2 {
		t.Errorf("expected 'b' to exist with value 2, got %v", val)
	}
}
