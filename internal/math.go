package internal

import (
	"errors"
	"math"
)

// Div performs integer division and returns an error if division by zero is attempted.
func Div(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}

// MeanAbsoluteDeviation computes the Mean Absolute Deviation around the mean.
// It returns 0 for empty slices.
func MeanAbsoluteDeviation(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0
	}

	// Calculate the mean
	sum := 0.0
	for _, num := range numbers {
		sum += num
	}
	mean := sum / float64(len(numbers))

	// Calculate the mean of absolute deviations from the mean
	sumAbsDev := 0.0
	for _, num := range numbers {
		sumAbsDev += math.Abs(num - mean)
	}
	return sumAbsDev / float64(len(numbers))
}
