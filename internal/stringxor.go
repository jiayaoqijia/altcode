package internal

// StringXOR performs XOR on two binary strings of the same length.
// StringXOR("010", "110") returns "100".
func StringXOR(a, b string) string {
	result := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		if a[i] == b[i] {
			result[i] = '0'
		} else {
			result[i] = '1'
		}
	}
	return string(result)
}
