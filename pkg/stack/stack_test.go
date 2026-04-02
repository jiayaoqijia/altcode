package stack_test

import (
	"testing"

	"github.com/altcode-ai/altcode/pkg/stack"
)

func TestStackPushPop(t *testing.T) {
	s := &internal.Stack[int]{}

	s.Push(1)
	s.Push(2)
	s.Push(3)

	if s.Len() != 3 {
		t.Errorf("expected len 3, got %d", s.Len())
	}

	val, ok := s.Pop()
	if !ok || val != 3 {
		t.Errorf("expected (3, true), got (%d, %v)", val, ok)
	}

	val, ok = s.Pop()
	if !ok || val != 2 {
		t.Errorf("expected (2, true), got (%d, %v)", val, ok)
	}

	val, ok = s.Pop()
	if !ok || val != 1 {
		t.Errorf("expected (1, true), got (%d, %v)", val, ok)
	}

	val, ok = s.Pop()
	if ok {
		t.Errorf("expected (_, false), got (%d, %v)", val, ok)
	}
}

func TestStackPeek(t *testing.T) {
	s := &internal.Stack[string]{}

	s.Push("hello")
	s.Push("world")

	val, ok := s.Peek()
	if !ok || val != "world" {
		t.Errorf("expected (world, true), got (%s, %v)", val, ok)
	}

	// Peek should not modify the stack
	if s.Len() != 2 {
		t.Errorf("expected len 2 after peek, got %d", s.Len())
	}
}

func TestStackIsEmpty(t *testing.T) {
	s := &internal.Stack[float64]{}

	if !s.IsEmpty() {
		t.Error("expected empty stack")
	}

	s.Push(1.5)
	if s.IsEmpty() {
		t.Error("expected non-empty stack")
	}

	s.Pop()
	if !s.IsEmpty() {
		t.Error("expected empty stack after pop")
	}
}

func TestStackLen(t *testing.T) {
	s := &internal.Stack[byte]{}

	if s.Len() != 0 {
		t.Errorf("expected len 0, got %d", s.Len())
	}

	for i := 0; i < 5; i++ {
		s.Push(byte(i))
		if s.Len() != i+1 {
			t.Errorf("expected len %d, got %d", i+1, s.Len())
		}
	}

	for i := 5; i > 0; i-- {
		s.Pop()
		if s.Len() != i-1 {
			t.Errorf("expected len %d, got %d", i-1, s.Len())
		}
	}
}

func TestStackWithDifferentTypes(t *testing.T) {
	// Test with interface{}
	sInterface := &internal.Stack[interface{}]{}
	sInterface.Push(42)
	sInterface.Push("test")
	sInterface.Push(3.14)

	if sInterface.Len() != 3 {
		t.Errorf("expected len 3, got %d", sInterface.Len())
	}

	val, ok := sInterface.Peek()
	if !ok || val != 3.14 {
		t.Errorf("expected (3.14, true), got (%v, %v)", val, ok)
	}
}
