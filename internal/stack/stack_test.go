package stack

import (
	"testing"
)

func TestStack_PushPop(t *testing.T) {
	s := &Stack[int]{}

	s.Push(1)
	s.Push(2)
	s.Push(3)

	if val := s.Pop(); val != 3 {
		t.Errorf("expected 3, got %d", val)
	}
	if val := s.Pop(); val != 2 {
		t.Errorf("expected 2, got %d", val)
	}
	if val := s.Pop(); val != 1 {
		t.Errorf("expected 1, got %d", val)
	}
}

func TestStack_Peek(t *testing.T) {
	s := &Stack[string]{}

	s.Push("a")
	s.Push("b")
	s.Push("c")

	// Peek should not modify the stack
	if val := s.Peek(); val != "c" {
		t.Errorf("expected c, got %s", val)
	}
	if val := s.Peek(); val != "c" {
		t.Errorf("expected c, got %s", val)
	}

	// Length should remain unchanged
	if s.Len() != 3 {
		t.Errorf("expected len 3, got %d", s.Len())
	}
}

func TestStack_Len(t *testing.T) {
	s := &Stack[float64]{}

	if s.Len() != 0 {
		t.Errorf("expected len 0, got %d", s.Len())
	}

	s.Push(1.5)
	if s.Len() != 1 {
		t.Errorf("expected len 1, got %d", s.Len())
	}

	s.Push(2.5)
	s.Push(3.5)
	if s.Len() != 3 {
		t.Errorf("expected len 3, got %d", s.Len())
	}

	s.Pop()
	if s.Len() != 2 {
		t.Errorf("expected len 2, got %d", s.Len())
	}
}

func TestStack_IsEmpty(t *testing.T) {
	s := &Stack[bool]{}

	if !s.IsEmpty() {
		t.Error("expected IsEmpty to be true for new stack")
	}

	s.Push(true)
	if s.IsEmpty() {
		t.Error("expected IsEmpty to be false after Push")
	}

	s.Pop()
	if !s.IsEmpty() {
		t.Error("expected IsEmpty to be true after Pop")
	}
}

func TestStack_PopEmpty(t *testing.T) {
	s := &Stack[int]{}

	// Pop on empty stack should return zero value
	val := s.Pop()
	if val != 0 {
		t.Errorf("expected zero value 0, got %d", val)
	}
}

func TestStack_PeekEmpty(t *testing.T) {
	s := &Stack[string]{}

	// Peek on empty stack should return zero value
	val := s.Peek()
	if val != "" {
		t.Errorf("expected zero value empty string, got %s", val)
	}
}

func TestStack_WithStructs(t *testing.T) {
	type Point struct {
		X int
		Y int
	}

	s := &Stack[Point]{}

	s.Push(Point{1, 2})
	s.Push(Point{3, 4})

	p := s.Pop()
	if p.X != 3 || p.Y != 4 {
		t.Errorf("expected Point{3, 4}, got Point{%d, %d}", p.X, p.Y)
	}

	p = s.Peek()
	if p.X != 1 || p.Y != 2 {
		t.Errorf("expected Point{1, 2}, got Point{%d, %d}", p.X, p.Y)
	}
}

func TestStack_LIFO(t *testing.T) {
	s := &Stack[int]{}

	// Push numbers 1-5
	for i := 1; i <= 5; i++ {
		s.Push(i)
	}

	// Pop should return in reverse order
	for i := 5; i >= 1; i-- {
		if val := s.Pop(); val != i {
			t.Errorf("expected %d, got %d", i, val)
		}
	}
}
