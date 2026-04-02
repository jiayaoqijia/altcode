package stack

// Stack is a generic LIFO (Last In First Out) data structure.
type Stack[T any] struct {
	items []T
}

// New creates a new empty Stack.
func New[T any]() *Stack[T] {
	return &Stack[T]{}
}

// Push adds an item to the top of the stack.
func (s *Stack[T]) Push(item T) {
	s.items = append(s.items, item)
}

// Pop removes and returns the top item from the stack.
// Returns the zero value of T and false if the stack is empty.
func (s *Stack[T]) Pop() (T, bool) {
	if s.IsEmpty() {
		var zero T
		return zero, false
	}
	item := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return item, true
}

// Peek returns the top item without removing it.
// Returns the zero value of T and false if the stack is empty.
func (s *Stack[T]) Peek() (T, bool) {
	if s.IsEmpty() {
		var zero T
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
