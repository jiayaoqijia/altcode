package internal

import (
	"testing"
)

func TestLRUCacheBasicOperations(t *testing.T) {
	lru := NewLRUCache(3)

	// Test Put and Get
	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3)

	val, ok := lru.Get("a")
	if !ok || val != 1 {
		t.Errorf("Expected Get('a') = 1, got %v, %v", val, ok)
	}

	if lru.Size() != 3 {
		t.Errorf("Expected size 3, got %d", lru.Size())
	}
}

func TestLRUCacheEviction(t *testing.T) {
	lru := NewLRUCache(2)

	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3) // Should evict "a"

	_, ok := lru.Get("a")
	if ok {
		t.Error("Expected 'a' to be evicted, but it still exists")
	}

	val, ok := lru.Get("c")
	if !ok || val != 3 {
		t.Errorf("Expected Get('c') = 3, got %v, %v", val, ok)
	}

	if lru.Size() != 2 {
		t.Errorf("Expected size 2, got %d", lru.Size())
	}
}

func TestLRUCacheMoveToFront(t *testing.T) {
	lru := NewLRUCache(3)

	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3)

	// Access "a" to move it to front
	lru.Get("a")

	// Add "d", should evict "b" (least recently used)
	lru.Put("d", 4)

	_, okB := lru.Get("b")
	if okB {
		t.Error("Expected 'b' to be evicted, but it still exists")
	}

	val, okA := lru.Get("a")
	if !okA || val != 1 {
		t.Errorf("Expected Get('a') = 1, got %v, %v", val, okA)
	}
}

func TestLRUCacheUpdate(t *testing.T) {
	lru := NewLRUCache(2)

	lru.Put("a", 1)
	lru.Put("b", 2)

	// Update "a" with new value
	lru.Put("a", 10)

	val, ok := lru.Get("a")
	if !ok || val != 10 {
		t.Errorf("Expected Get('a') = 10, got %v, %v", val, ok)
	}

	// Since "a" was updated, it's most recent, "b" should be evicted next
	lru.Put("c", 3)

	_, okB := lru.Get("b")
	if okB {
		t.Error("Expected 'b' to be evicted, but it still exists")
	}
}

func TestLRUCacheNonExistent(t *testing.T) {
	lru := NewLRUCache(2)

	val, ok := lru.Get("nonexistent")
	if ok || val != nil {
		t.Errorf("Expected Get('nonexistent') to return false, got %v, %v", val, ok)
	}
}

func TestLRUCacheClear(t *testing.T) {
	lru := NewLRUCache(3)

	lru.Put("a", 1)
	lru.Put("b", 2)
	lru.Put("c", 3)

	lru.Clear()

	if lru.Size() != 0 {
		t.Errorf("Expected size 0 after Clear(), got %d", lru.Size())
	}

	_, ok := lru.Get("a")
	if ok {
		t.Error("Expected Get('a') to fail after Clear(), but it succeeded")
	}
}

func TestLRUCacheCapacityOne(t *testing.T) {
	lru := NewLRUCache(1)

	lru.Put("a", 1)
	val, ok := lru.Get("a")
	if !ok || val != 1 {
		t.Errorf("Expected Get('a') = 1, got %v, %v", val, ok)
	}

	lru.Put("b", 2)
	_, okA := lru.Get("a")
	if okA {
		t.Error("Expected 'a' to be evicted when 'b' is added")
	}

	val, okB := lru.Get("b")
	if !okB || val != 2 {
		t.Errorf("Expected Get('b') = 2, got %v, %v", val, okB)
	}
}

func TestLRUCacheMultipleTypes(t *testing.T) {
	lru := NewLRUCache(3)

	lru.Put("int", 42)
	lru.Put("string", "hello")
	lru.Put("list", []int{1, 2, 3})

	val, _ := lru.Get("int")
	if val != 42 {
		t.Errorf("Expected int value 42, got %v", val)
	}

	val, _ = lru.Get("string")
	if val != "hello" {
		t.Errorf("Expected string 'hello', got %v", val)
	}

	val, _ = lru.Get("list")
	if list, ok := val.([]int); !ok || len(list) != 3 {
		t.Errorf("Expected list with 3 elements, got %v", val)
	}
}
