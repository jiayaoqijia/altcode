package internal

import "errors"

// Div performs integer division and returns the result and an error.
// It returns an error if the divisor is zero.
func Div(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}
