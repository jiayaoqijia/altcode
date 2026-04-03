package internal

func HowManyTimes(s, sub string) int {
	if len(sub) == 0 || len(sub) > len(s) {
		return 0
	}
	count := 0
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}
	return count
}
