package internal

import "golang.org/x/exp/constraints"

// Max returns the maximum of two ordered values.
// Works with any type that satisfies the constraints.Ordered constraint:
// integers, floats, and strings.
func Max[T constraints.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

// Min returns the minimum of two ordered values.
// Works with any type that satisfies the constraints.Ordered constraint.
func Min[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	}
	return b
}

// Clamp returns a value clamped between min and max bounds.
func Clamp[T constraints.Ordered](value, min, max T) T {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
