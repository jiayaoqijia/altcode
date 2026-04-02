package internal

import (
	"testing"
)

func TestStackInt(t *testing.T) {
	s := &Stack[int]{}

	// Test IsEmpty on empty stack
	if !s.IsEmpty() {
		t.Error("expected empty stack")
	}

	// Test Len on empty stack
	if s.Len() != 0 {
		t.Error("expected len 0")
	}

	// Test Peek on empty stack
	if _, ok := s.Peek(); ok {
		t.Error("expected Peek to return false on empty stack")
	}

	// Test Push
	s.Push(1)
	s.Push(2)
	s.Push(3)

	if s.Len() != 3 {
		t.Errorf("expected len 3, got %d", s.Len())
	}

	if s.IsEmpty() {
		t.Error("expected non-empty stack")
	}

	// Test Peek
	val, ok := s.Peek()
	if !ok || val != 3 {
		t.Errorf("expected Peek to return 3, got %v", val)
	}

	// Test Pop
	val, ok = s.Pop()
	if !ok || val != 3 {
		t.Errorf("expected Pop to return 3, got %v", val)
	}

	val, ok = s.Pop()
	if !ok || val != 2 {
		t.Errorf("expected Pop to return 2, got %v", val)
	}

	val, ok = s.Pop()
	if !ok || val != 1 {
		t.Errorf("expected Pop to return 1, got %v", val)
	}

	// Test Pop on empty stack
	if _, ok := s.Pop(); ok {
		t.Error("expected Pop to return false on empty stack")
	}

	if !s.IsEmpty() {
		t.Error("expected empty stack after all pops")
	}
}

func TestStackString(t *testing.T) {
	s := &Stack[string]{}

	s.Push("hello")
	s.Push("world")

	if s.Len() != 2 {
		t.Errorf("expected len 2, got %d", s.Len())
	}

	val, ok := s.Peek()
	if !ok || val != "world" {
		t.Errorf("expected Peek to return 'world', got %v", val)
	}

	val, ok = s.Pop()
	if !ok || val != "world" {
		t.Errorf("expected Pop to return 'world', got %v", val)
	}

	val, ok = s.Pop()
	if !ok || val != "hello" {
		t.Errorf("expected Pop to return 'hello', got %v", val)
	}
}

func TestStackStructs(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	s := &Stack[Person]{}

	s.Push(Person{"Alice", 30})
	s.Push(Person{"Bob", 25})

	if s.Len() != 2 {
		t.Errorf("expected len 2, got %d", s.Len())
	}

	person, ok := s.Pop()
	if !ok || person.Name != "Bob" {
		t.Errorf("expected Pop to return Bob, got %v", person)
	}
}
