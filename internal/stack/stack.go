package stack

import "fmt"

// Stack is a generic last-in-first-out (LIFO) data structure.
type Stack[T any] struct {
	items []T
}

// New creates and returns a new empty Stack.
func New[T any]() *Stack[T] {
	return &Stack[T]{
		items: make([]T, 0),
	}
}

// Push adds an element to the top of the stack.
func (s *Stack[T]) Push(value T) {
	s.items = append(s.items, value)
}

// Pop removes and returns the top element from the stack.
// Returns an error if the stack is empty.
func (s *Stack[T]) Pop() (T, error) {
	var zero T
	if len(s.items) == 0 {
		return zero, fmt.Errorf("stack is empty")
	}
	lastIndex := len(s.items) - 1
	value := s.items[lastIndex]
	s.items = s.items[:lastIndex]
	return value, nil
}

// Peek returns the top element without removing it.
// Returns an error if the stack is empty.
func (s *Stack[T]) Peek() (T, error) {
	var zero T
	if len(s.items) == 0 {
		return zero, fmt.Errorf("stack is empty")
	}
	return s.items[len(s.items)-1], nil
}

// Len returns the number of elements in the stack.
func (s *Stack[T]) Len() int {
	return len(s.items)
}

// IsEmpty returns true if the stack contains no elements.
func (s *Stack[T]) IsEmpty() bool {
	return len(s.items) == 0
}
