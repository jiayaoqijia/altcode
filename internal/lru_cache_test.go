package internal

import (
	"testing"
)

func TestLRUCacheBasic(t *testing.T) {
	cache := NewLRUCache(2)

	// Test Put and Get
	cache.Put("a", 1)
	cache.Put("b", 2)

	if v, ok := cache.Get("a"); !ok || v != 1 {
		t.Errorf("Expected Get('a') = 1, got %v, ok=%v", v, ok)
	}

	if v, ok := cache.Get("b"); !ok || v != 2 {
		t.Errorf("Expected Get('b') = 2, got %v, ok=%v", v, ok)
	}
}

func TestLRUCacheEviction(t *testing.T) {
	cache := NewLRUCache(2)

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("c", 3) // Should evict 'a' (least recently used)

	if _, ok := cache.Get("a"); ok {
		t.Errorf("Expected 'a' to be evicted, but it still exists")
	}

	if v, ok := cache.Get("b"); !ok || v != 2 {
		t.Errorf("Expected Get('b') = 2, got %v, ok=%v", v, ok)
	}

	if v, ok := cache.Get("c"); !ok || v != 3 {
		t.Errorf("Expected Get('c') = 3, got %v, ok=%v", v, ok)
	}
}

func TestLRUCacheUpdateAccess(t *testing.T) {
	cache := NewLRUCache(2)

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Get("a") // Access 'a', making it recently used
	cache.Put("c", 3) // Should evict 'b' (now least recently used)

	if v, ok := cache.Get("a"); !ok || v != 1 {
		t.Errorf("Expected Get('a') = 1, got %v, ok=%v", v, ok)
	}

	if _, ok := cache.Get("b"); ok {
		t.Errorf("Expected 'b' to be evicted, but it still exists")
	}

	if v, ok := cache.Get("c"); !ok || v != 3 {
		t.Errorf("Expected Get('c') = 3, got %v, ok=%v", v, ok)
	}
}

func TestLRUCacheUpdate(t *testing.T) {
	cache := NewLRUCache(2)

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Put("a", 100) // Update 'a', moving it to front

	if v, ok := cache.Get("a"); !ok || v != 100 {
		t.Errorf("Expected Get('a') = 100, got %v, ok=%v", v, ok)
	}

	cache.Put("c", 3) // Should evict 'b' (least recently used)

	if _, ok := cache.Get("b"); ok {
		t.Errorf("Expected 'b' to be evicted, but it still exists")
	}
}

func TestLRUCacheGetNonExistent(t *testing.T) {
	cache := NewLRUCache(2)

	if v, ok := cache.Get("nonexistent"); ok {
		t.Errorf("Expected Get('nonexistent') to return false, got ok=%v, v=%v", ok, v)
	}
}

func TestLRUCacheCapacityOne(t *testing.T) {
	cache := NewLRUCache(1)

	cache.Put("a", 1)
	if v, ok := cache.Get("a"); !ok || v != 1 {
		t.Errorf("Expected Get('a') = 1, got %v, ok=%v", v, ok)
	}

	cache.Put("b", 2) // Should evict 'a'
	if _, ok := cache.Get("a"); ok {
		t.Errorf("Expected 'a' to be evicted")
	}

	if v, ok := cache.Get("b"); !ok || v != 2 {
		t.Errorf("Expected Get('b') = 2, got %v, ok=%v", v, ok)
	}
}

func TestLRUCacheLen(t *testing.T) {
	cache := NewLRUCache(3)

	cache.Put("a", 1)
	if cache.Len() != 1 {
		t.Errorf("Expected Len() = 1, got %d", cache.Len())
	}

	cache.Put("b", 2)
	cache.Put("c", 3)
	if cache.Len() != 3 {
		t.Errorf("Expected Len() = 3, got %d", cache.Len())
	}

	cache.Put("d", 4) // Evict one
	if cache.Len() != 3 {
		t.Errorf("Expected Len() = 3 after eviction, got %d", cache.Len())
	}
}

func TestLRUCacheClear(t *testing.T) {
	cache := NewLRUCache(2)

	cache.Put("a", 1)
	cache.Put("b", 2)
	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("Expected Len() = 0 after Clear(), got %d", cache.Len())
	}

	if _, ok := cache.Get("a"); ok {
		t.Errorf("Expected 'a' to not exist after Clear()")
	}
}
