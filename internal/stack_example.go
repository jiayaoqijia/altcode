package internal

import "fmt"

// ExampleStackUsage demonstrates the Stack[T] usage with different types.
func ExampleStackUsage() {
	// Example 1: Stack of integers
	intStack := &Stack[int]{}
	intStack.Push(10)
	intStack.Push(20)
	intStack.Push(30)

	fmt.Println("Integer Stack:")
	fmt.Printf("Len: %d, IsEmpty: %v\n", intStack.Len(), intStack.IsEmpty())

	if val, ok := intStack.Peek(); ok {
		fmt.Printf("Peek: %d\n", val)
	}

	for intStack.Len() > 0 {
		if val, ok := intStack.Pop(); ok {
			fmt.Printf("Popped: %d\n", val)
		}
	}

	fmt.Printf("After pop-all - Len: %d, IsEmpty: %v\n\n", intStack.Len(), intStack.IsEmpty())

	// Example 2: Stack of strings
	strStack := &Stack[string]{}
	strStack.Push("hello")
	strStack.Push("world")

	fmt.Println("String Stack:")
	for strStack.Len() > 0 {
		if val, ok := strStack.Pop(); ok {
			fmt.Printf("Popped: %s\n", val)
		}
	}

	// Example 3: Pop from empty stack
	emptyStack := &Stack[float64]{}
	if val, ok := emptyStack.Pop(); !ok {
		fmt.Printf("\nAttempted pop from empty stack - received zero value: %v\n", val)
	}
}
