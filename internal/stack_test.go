package stack

import (
	"testing"
)

func TestStack_PushPop(t *testing.T) {
	s := Stack[int]{}
	s.Push(1)
	s.Push(2)
	s.Push(3)

	if s.Len() != 3 {
		t.Errorf("expected length 3, got %d", s.Len())
	}

	item, ok := s.Pop()
	if !ok || item != 3 {
		t.Errorf("expected pop 3, got %v, ok=%v", item, ok)
	}

	if s.Len() != 2 {
		t.Errorf("expected length 2 after pop, got %d", s.Len())
	}
}

func TestStack_Peek(t *testing.T) {
	s := Stack[string]{}
	s.Push("hello")
	s.Push("world")

	item, ok := s.Peek()
	if !ok || item != "world" {
		t.Errorf("expected peek 'world', got %v, ok=%v", item, ok)
	}

	// Verify Peek doesn't remove item
	if s.Len() != 2 {
		t.Errorf("expected length 2 after peek, got %d", s.Len())
	}
}

func TestStack_IsEmpty(t *testing.T) {
	s := Stack[int]{}

	if !s.IsEmpty() {
		t.Error("expected empty stack")
	}

	s.Push(1)
	if s.IsEmpty() {
		t.Error("expected non-empty stack")
	}
}

func TestStack_PopEmpty(t *testing.T) {
	s := Stack[int]{}
	item, ok := s.Pop()
	if ok {
		t.Error("expected false for pop from empty stack")
	}
	_ = item // zero value
}

func TestStack_PeekEmpty(t *testing.T) {
	s := Stack[int]{}
	item, ok := s.Peek()
	if ok {
		t.Error("expected false for peek from empty stack")
	}
	_ = item // zero value
}

func TestStack_PointerType(t *testing.T) {
	type TestStruct struct {
		Value int
	}
	s := Stack[*TestStruct]{}
	ts := &TestStruct{Value: 42}
	s.Push(ts)

	item, ok := s.Pop()
	if !ok || item.Value != 42 {
		t.Errorf("expected pointer with Value=42, got %v, ok=%v", item, ok)
	}
}
