package lru

import (
	"testing"
)

func TestLRUCache_GetAndPut(t *testing.T) {
	cache := New[string, int](2)

	// Test get non-existent key
	value, found := cache.Get("nonexistent")
	if found {
		t.Error("Expected not found for non-existent key")
	}
	if value != 0 {
		t.Errorf("Expected zero value, got %d", value)
	}

	// Test put and get
	cache.Put("a", 1)
	value, found = cache.Get("a")
	if !found {
		t.Error("Expected found for existing key")
	}
	if value != 1 {
		t.Errorf("Expected 1, got %d", value)
	}

	// Test update existing key
	cache.Put("a", 2)
	value, found = cache.Get("a")
	if !found {
		t.Error("Expected found for existing key")
	}
	if value != 2 {
		t.Errorf("Expected 2, got %d", value)
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	cache := New[string, int](2)

	cache.Put("a", 1)
	cache.Put("b", 2)

	// Cache should be at capacity
	if cache.Len() != 2 {
		t.Errorf("Expected length 2, got %d", cache.Len())
	}

	// Add new item, should evict "a" (LRU)
	cache.Put("c", 3)

	// "a" should be evicted
	_, found := cache.Get("a")
	if found {
		t.Error("Expected 'a' to be evicted")
	}

	// "b" and "c" should still be there
	if value, found := cache.Get("b"); !found || value != 2 {
		t.Errorf("Expected 'b' to still be in cache with value 2")
	}
	if value, found := cache.Get("c"); !found || value != 3 {
		t.Errorf("Expected 'c' to still be in cache with value 3")
	}
}

func TestLRUCache_MRUOrder(t *testing.T) {
	cache := New[string, int](3)

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3)

	// Access "a" to make it most recently used
	cache.Get("a")

	// Add new item, should evict "b" (now LRU)
	cache.Put("d", 4)

	// "b" should be evicted
	_, found := cache.Get("b")
	if found {
		t.Error("Expected 'b' to be evicted")
	}

	// "a", "c", "d" should remain
	for _, key := range []string{"a", "c", "d"} {
		if _, found := cache.Get(key); !found {
			t.Errorf("Expected '%s' to still be in cache", key)
		}
	}
}

func TestLRUCache_Overwrite(t *testing.T) {
	cache := New[string, int](2)

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("a", 10) // Update "a"

	// Should still have both, "b" should be LRU
	if value, found := cache.Get("a"); !found || value != 10 {
		t.Errorf("Expected 'a' to have value 10")
	}
	if value, found := cache.Get("b"); !found || value != 2 {
		t.Errorf("Expected 'b' to have value 2")
	}

	// Add new item, should evict "b" (LRU now)
	cache.Put("c", 3)

	_, found := cache.Get("b")
	if found {
		t.Error("Expected 'b' to be evicted")
	}
}

func TestLRUCache_EmptyCache(t *testing.T) {
	cache := New[string, int](0)

	cache.Put("a", 1)

	// With capacity 0, item should not be stored
	_, found := cache.Get("a")
	if found {
		t.Error("Expected empty cache to not store items")
	}
}
