package internal

import "math"

func MeanAbsoluteDeviation(numbers []float64) float64 {
	if len(numbers) == 0 {
		return 0
	}

	// Calculate mean
	var sum float64
	for _, n := range numbers {
		sum += n
	}
	mean := sum / float64(len(numbers))

	// Calculate mean of absolute deviations
	var absSum float64
	for _, n := range numbers {
		absSum += math.Abs(n - mean)
	}

	return absSum / float64(len(numbers))
}
