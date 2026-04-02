package internal

func GreatestCommonDivisor(a, b int) int {
	a, b = abs(a), abs(b)
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
