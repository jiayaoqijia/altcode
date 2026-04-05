package internal

import "golang.org/x/exp/constraints"

// Max returns the maximum of two ordered values.
// Works with any type that satisfies the Ordered constraint (int, float64, string, etc.)
func Max[T constraints.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// Min returns the minimum of two ordered values.
func Min[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}
