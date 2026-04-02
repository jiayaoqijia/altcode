package stack

import "testing"

func TestStack(t *testing.T) {
	s := New[int]()

	// Empty stack
	if s.Len() != 0 {
		t.Errorf("expected 0, got %d", s.Len())
	}
	if _, ok := s.Pop(); ok {
		t.Error("expected false on empty pop")
	}
	if _, ok := s.Peek(); ok {
		t.Error("expected false on empty peek")
	}

	// Push and peek
	s.Push(1)
	s.Push(2)
	s.Push(3)
	if s.Len() != 3 {
		t.Errorf("expected 3, got %d", s.Len())
	}
	if v, ok := s.Peek(); !ok || v != 3 {
		t.Errorf("expected 3, got %d, ok %v", v, ok)
	}

	// Pop in LIFO order
	if v, ok := s.Pop(); !ok || v != 3 {
		t.Errorf("expected 3, got %d, ok %v", v, ok)
	}
	if v, ok := s.Pop(); !ok || v != 2 {
		t.Errorf("expected 2, got %d, ok %v", v, ok)
	}
	if v, ok := s.Pop(); !ok || v != 1 {
		t.Errorf("expected 1, got %d, ok %v", v, ok)
	}
	if s.Len() != 0 {
		t.Errorf("expected 0, got %d", s.Len())
	}
}

func TestStackGeneric(t *testing.T) {
	// String stack
	ss := New[string]()
	ss.Push("a")
	ss.Push("b")
	if v, ok := ss.Pop(); !ok || v != "b" {
		t.Errorf("expected b, got %s, ok %v", v, ok)
	}
}
