package stack

// Stack is a generic LIFO data structure.
type Stack[T any] struct {
	items []T
}

// New creates a new empty stack.
func New[T any]() *Stack[T] {
	return &Stack[T]{items: make([]T, 0)}
}

// Push adds an item to the top of the stack.
func (s *Stack[T]) Push(item T) {
	s.items = append(s.items, item)
}

// Pop removes and returns the top item. Returns zero value and false if empty.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	n := len(s.items) - 1
	item := s.items[n]
	s.items[n] = zero // avoid memory leak
	s.items = s.items[:n]
	return item, true
}

// Peek returns the top item without removing it. Returns zero value and false if empty.
func (s *Stack[T]) Peek() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	return s.items[len(s.items)-1], true
}

// Len returns the number of items in the stack.
func (s *Stack[T]) Len() int {
	return len(s.items)
}

// IsEmpty returns true if the stack has no items.
func (s *Stack[T]) IsEmpty() bool {
	return len(s.items) == 0
}
