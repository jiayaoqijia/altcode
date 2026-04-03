package internal

import (
	"testing"
)

func TestStackPush(t *testing.T) {
	stack := &Stack[int]{}
	stack.Push(1)
	stack.Push(2)
	stack.Push(3)

	if stack.Len() != 3 {
		t.Errorf("expected length 3, got %d", stack.Len())
	}
}

func TestStackPop(t *testing.T) {
	stack := &Stack[int]{}
	stack.Push(10)
	stack.Push(20)
	stack.Push(30)

	val, ok := stack.Pop()
	if !ok || val != 30 {
		t.Errorf("expected 30, got %d", val)
	}

	val, ok = stack.Pop()
	if !ok || val != 20 {
		t.Errorf("expected 20, got %d", val)
	}

	val, ok = stack.Pop()
	if !ok || val != 10 {
		t.Errorf("expected 10, got %d", val)
	}

	val, ok = stack.Pop()
	if ok {
		t.Error("expected pop from empty stack to return false")
	}
}

func TestStackPeek(t *testing.T) {
	stack := &Stack[string]{}
	stack.Push("a")
	stack.Push("b")

	val, ok := stack.Peek()
	if !ok || val != "b" {
		t.Errorf("expected 'b', got %s", val)
	}

	// Peek should not remove the element
	if stack.Len() != 2 {
		t.Errorf("expected length 2 after peek, got %d", stack.Len())
	}

	val, ok = stack.Peek()
	if !ok || val != "b" {
		t.Errorf("expected 'b' again, got %s", val)
	}
}

func TestStackPeekEmpty(t *testing.T) {
	stack := &Stack[int]{}

	val, ok := stack.Peek()
	if ok {
		t.Error("expected peek on empty stack to return false")
	}

	if val != 0 {
		t.Errorf("expected zero value, got %d", val)
	}
}

func TestStackLen(t *testing.T) {
	stack := &Stack[int]{}

	if stack.Len() != 0 {
		t.Error("new stack should have length 0")
	}

	stack.Push(1)
	stack.Push(2)
	if stack.Len() != 2 {
		t.Errorf("expected length 2, got %d", stack.Len())
	}

	stack.Pop()
	if stack.Len() != 1 {
		t.Errorf("expected length 1, got %d", stack.Len())
	}
}

func TestStackIsEmpty(t *testing.T) {
	stack := &Stack[int]{}

	if !stack.IsEmpty() {
		t.Error("new stack should be empty")
	}

	stack.Push(1)
	if stack.IsEmpty() {
		t.Error("stack with 1 element should not be empty")
	}

	stack.Pop()
	if !stack.IsEmpty() {
		t.Error("stack should be empty after popping only element")
	}
}

func TestStackWithDifferentTypes(t *testing.T) {
	// Test with float64
	floatStack := &Stack[float64]{}
	floatStack.Push(3.14)
	floatStack.Push(2.71)

	val, ok := floatStack.Pop()
	if !ok || val != 2.71 {
		t.Errorf("expected 2.71, got %f", val)
	}

	// Test with structs
	type Person struct {
		Name string
		Age  int
	}

	personStack := &Stack[Person]{}
	personStack.Push(Person{"Alice", 30})
	personStack.Push(Person{"Bob", 25})

	val2, ok := personStack.Pop()
	if !ok || val2.Name != "Bob" {
		t.Errorf("expected Bob, got %s", val2.Name)
	}
}

func BenchmarkStackPush(b *testing.B) {
	stack := &Stack[int]{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stack.Push(i)
	}
}

func BenchmarkStackPop(b *testing.B) {
	stack := &Stack[int]{}
	for i := 0; i < b.N; i++ {
		stack.Push(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stack.Pop()
	}
}
