package stack

// Stack[T] is a generic LIFO (Last In First Out) data structure.
type Stack[T any] struct {
	items []T
}

// Push adds an element to the top of the stack.
func (s *Stack[T]) Push(value T) {
	s.items = append(s.items, value)
}

// Pop removes and returns the top element from the stack.
// Returns the zero value for T if the stack is empty.
// Check IsEmpty() before calling if you need to handle empty stack cases.
func (s *Stack[T]) Pop() T {
	var zero T
	if len(s.items) == 0 {
		return zero
	}
	value := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return value
}

// Peek returns the top element without removing it.
// Returns the zero value for T if the stack is empty.
// Check IsEmpty() before calling if you need to handle empty stack cases.
func (s *Stack[T]) Peek() T {
	var zero T
	if len(s.items) == 0 {
		return zero
	}
	return s.items[len(s.items)-1]
}

// Len returns the number of elements in the stack.
func (s *Stack[T]) Len() int {
	return len(s.items)
}

// IsEmpty returns true if the stack contains no elements.
func (s *Stack[T]) IsEmpty() bool {
	return len(s.items) == 0
}
