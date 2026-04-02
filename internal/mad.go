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

	// Calculate mean absolute deviation
	var devSum float64
	for _, n := range numbers {
		devSum += math.Abs(n - mean)
	}

	return devSum / float64(len(numbers))
}
