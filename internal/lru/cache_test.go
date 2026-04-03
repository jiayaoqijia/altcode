package lru

import (
	"testing"
)

func TestGetAndPut(t *testing.T) {
	c := New(2)

	// Test Put and Get
	c.Put("a", 1)
	val, ok := c.Get("a")
	if !ok || val != 1 {
		t.Errorf("expected (1, true), got (%v, %v)", val, ok)
	}

	// Test missing key
	val, ok = c.Get("missing")
	if ok {
		t.Errorf("expected (nil, false) for missing key, got (%v, %v)", val, ok)
	}
}

func TestEviction(t *testing.T) {
	c := New(2)

	c.Put("a", 1)
	c.Put("b", 2)
	if c.Len() != 2 {
		t.Errorf("expected len 2, got %d", c.Len())
	}

	// Adding third item should evict "a" (LRU)
	c.Put("c", 3)
	if c.Len() != 2 {
		t.Errorf("expected len 2 after eviction, got %d", c.Len())
	}

	// "a" should be evicted
	if _, ok := c.Get("a"); ok {
		t.Error("expected 'a' to be evicted")
	}

	// "b" and "c" should exist
	if _, ok := c.Get("b"); !ok {
		t.Error("expected 'b' to exist")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("expected 'c' to exist")
	}
}

func TestRecencyOrder(t *testing.T) {
	c := New(3)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	// Access "a" to make it recently used
	c.Get("a")

	// Add new item, should evict "b" (now LRU)
	c.Put("d", 4)

	if _, ok := c.Get("b"); ok {
		t.Error("expected 'b' to be evicted")
	}

	if _, ok := c.Get("a"); !ok {
		t.Error("expected 'a' to exist")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("expected 'c' to exist")
	}
	if _, ok := c.Get("d"); !ok {
		t.Error("expected 'd' to exist")
	}
}

func TestUpdate(t *testing.T) {
	c := New(2)

	c.Put("a", 1)
	c.Put("b", 2)

	// Update existing key
	c.Put("a", 10)

	val, ok := c.Get("a")
	if !ok || val != 10 {
		t.Errorf("expected (10, true), got (%v, %v)", val, ok)
	}

	// Length should remain 2
	if c.Len() != 2 {
		t.Errorf("expected len 2, got %d", c.Len())
	}

	// "a" should now be most recent, so "b" gets evicted next
	c.Put("c", 3)
	if _, ok := c.Get("b"); ok {
		t.Error("expected 'b' to be evicted")
	}
}

func TestClear(t *testing.T) {
	c := New(2)
	c.Put("a", 1)
	c.Put("b", 2)

	c.Clear()

	if c.Len() != 0 {
		t.Errorf("expected len 0 after clear, got %d", c.Len())
	}

	if _, ok := c.Get("a"); ok {
		t.Error("expected cache to be empty")
	}
}

func TestCapacityOne(t *testing.T) {
	c := New(1)

	c.Put("a", 1)
	c.Put("b", 2)

	if c.Len() != 1 {
		t.Errorf("expected len 1, got %d", c.Len())
	}

	if _, ok := c.Get("a"); ok {
		t.Error("expected 'a' to be evicted")
	}

	val, ok := c.Get("b")
	if !ok || val != 2 {
		t.Errorf("expected (2, true), got (%v, %v)", val, ok)
	}
}

func TestZeroOrNegativeCapacity(t *testing.T) {
	c := New(0)
	if c.capacity != 1 {
		t.Errorf("expected capacity 1 for 0 input, got %d", c.capacity)
	}

	c = New(-5)
	if c.capacity != 1 {
		t.Errorf("expected capacity 1 for -5 input, got %d", c.capacity)
	}
}

func TestVariousTypes(t *testing.T) {
	c := New(3)

	c.Put("int", 42)
	c.Put("string", "hello")
	c.Put("slice", []int{1, 2, 3})

	val, _ := c.Get("int")
	if val != 42 {
		t.Errorf("expected 42, got %v", val)
	}

	val, _ = c.Get("string")
	if val != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}

	val, _ = c.Get("slice")
	if len(val.([]int)) != 3 {
		t.Errorf("expected slice len 3, got %v", val)
	}
}
