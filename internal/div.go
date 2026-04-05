package internal

import "errors"

// Div performs integer division of a by b.
// It returns the quotient and an error if b is zero.
func Div(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}
