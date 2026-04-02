package stack

import (
	"testing"
)

func TestStackInt(t *testing.T) {
	s := New[int]()

	// Test IsEmpty on new stack
	if !s.IsEmpty() {
		t.Error("new stack should be empty")
	}

	// Test Len on new stack
	if s.Len() != 0 {
		t.Error("new stack length should be 0")
	}

	// Test Push and Len
	s.Push(1)
	s.Push(2)
	s.Push(3)
	if s.Len() != 3 {
		t.Errorf("expected length 3, got %d", s.Len())
	}

	// Test Peek
	val, err := s.Peek()
	if err != nil {
		t.Errorf("peek failed: %v", err)
	}
	if val != 3 {
		t.Errorf("expected peek value 3, got %d", val)
	}
	if s.Len() != 3 {
		t.Error("peek should not change length")
	}

	// Test Pop
	val, err = s.Pop()
	if err != nil {
		t.Errorf("pop failed: %v", err)
	}
	if val != 3 {
		t.Errorf("expected pop value 3, got %d", val)
	}
	if s.Len() != 2 {
		t.Errorf("expected length 2 after pop, got %d", s.Len())
	}

	// Test Pop all
	s.Pop()
	s.Pop()
	if !s.IsEmpty() {
		t.Error("stack should be empty after popping all elements")
	}

	// Test Pop on empty stack
	_, err = s.Pop()
	if err == nil {
		t.Error("pop on empty stack should return error")
	}

	// Test Peek on empty stack
	_, err = s.Peek()
	if err == nil {
		t.Error("peek on empty stack should return error")
	}
}

func TestStackString(t *testing.T) {
	s := New[string]()

	s.Push("hello")
	s.Push("world")

	val, _ := s.Pop()
	if val != "world" {
		t.Errorf("expected 'world', got '%s'", val)
	}

	val, _ = s.Peek()
	if val != "hello" {
		t.Errorf("expected 'hello', got '%s'", val)
	}

	if s.Len() != 1 {
		t.Errorf("expected length 1, got %d", s.Len())
	}
}

func TestStackStruct(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	s := New[Person]()

	s.Push(Person{"Alice", 30})
	s.Push(Person{"Bob", 25})

	val, _ := s.Pop()
	if val.Name != "Bob" || val.Age != 25 {
		t.Errorf("expected Bob (25), got %s (%d)", val.Name, val.Age)
	}

	if s.Len() != 1 {
		t.Errorf("expected length 1, got %d", s.Len())
	}
}
