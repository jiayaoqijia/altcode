package lru

import (
	"testing"
)

func TestGet(t *testing.T) {
	cache := New(2)

	// Test non-existent key
	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected false for non-existent key")
	}

	// Add and retrieve
	cache.Put("key1", "value1")
	val, ok := cache.Get("key1")
	if !ok || val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}
}

func TestPut(t *testing.T) {
	cache := New(2)

	// Add items
	cache.Put("key1", "value1")
	cache.Put("key2", "value2")

	if cache.Len() != 2 {
		t.Errorf("expected length 2, got %d", cache.Len())
	}

	// Update existing key
	cache.Put("key1", "updated1")
	val, _ := cache.Get("key1")
	if val != "updated1" {
		t.Errorf("expected updated1, got %v", val)
	}
}

func TestEviction(t *testing.T) {
	cache := New(2)

	// Fill cache to capacity
	cache.Put("key1", "value1")
	cache.Put("key2", "value2")

	// Add third item, should evict LRU (key1)
	cache.Put("key3", "value3")

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected key1 to be evicted")
	}

	val2, ok := cache.Get("key2")
	if !ok || val2 != "value2" {
		t.Errorf("expected key2 to exist with value2, got %v", val2)
	}

	val3, ok := cache.Get("key3")
	if !ok || val3 != "value3" {
		t.Errorf("expected key3 to exist with value3, got %v", val3)
	}
}

func TestLRUOrdering(t *testing.T) {
	cache := New(3)

	// Add three items
	cache.Put("key1", "value1")
	cache.Put("key2", "value2")
	cache.Put("key3", "value3")

	// Access key1, making it recently used
	cache.Get("key1")

	// Add key4, should evict key2 (least recently used)
	cache.Put("key4", "value4")

	_, ok := cache.Get("key2")
	if ok {
		t.Error("expected key2 to be evicted")
	}

	// Verify other keys exist
	for _, key := range []string{"key1", "key3", "key4"} {
		_, ok := cache.Get(key)
		if !ok {
			t.Errorf("expected %s to exist", key)
		}
	}
}

func TestUpdateMovesToFront(t *testing.T) {
	cache := New(3)

	cache.Put("key1", "value1")
	cache.Put("key2", "value2")
	cache.Put("key3", "value3")

	// Update key1, making it recently used
	cache.Put("key1", "updated1")

	// Add key4, should evict key2 (least recently used)
	cache.Put("key4", "value4")

	_, ok := cache.Get("key2")
	if ok {
		t.Error("expected key2 to be evicted")
	}

	val1, ok := cache.Get("key1")
	if !ok || val1 != "updated1" {
		t.Errorf("expected key1 with updated1, got %v", val1)
	}
}

func TestDifferentValueTypes(t *testing.T) {
	cache := New(3)

	// Test with different types
	cache.Put("str", "string_value")
	cache.Put("num", 42)
	cache.Put("obj", map[string]int{"a": 1})

	if v, ok := cache.Get("str"); !ok || v != "string_value" {
		t.Errorf("expected string_value, got %v", v)
	}

	if v, ok := cache.Get("num"); !ok || v != 42 {
		t.Errorf("expected 42, got %v", v)
	}

	_, ok = cache.Get("obj")
	if !ok {
		t.Errorf("expected object to exist")
	}
}

func TestCapacityOne(t *testing.T) {
	cache := New(1)

	cache.Put("key1", "value1")
	val, ok := cache.Get("key1")
	if !ok || val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}

	cache.Put("key2", "value2")

	_, ok = cache.Get("key1")
	if ok {
		t.Error("expected key1 to be evicted")
	}

	val, ok = cache.Get("key2")
	if !ok || val != "value2" {
		t.Errorf("expected value2, got %v", val)
	}
}

func TestClear(t *testing.T) {
	cache := New(2)

	cache.Put("key1", "value1")
	cache.Put("key2", "value2")

	cache.Clear()

	if cache.Len() != 0 {
		t.Errorf("expected length 0 after clear, got %d", cache.Len())
	}

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected key1 to not exist after clear")
	}
}
