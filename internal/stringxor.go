package internal

// StringXOR performs XOR on two binary strings of the same length.
// Example: StringXOR("010", "110") returns "100"
func StringXOR(a, b string) string {
	result := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		// XOR: '0' XOR '0' = '0', '0' XOR '1' = '1', '1' XOR '0' = '1', '1' XOR '1' = '0'
		if (a[i] == '0' && b[i] == '1') || (a[i] == '1' && b[i] == '0') {
			result[i] = '1'
		} else {
			result[i] = '0'
		}
	}
	return string(result)
}
