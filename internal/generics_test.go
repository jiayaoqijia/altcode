package internal

import "testing"

// TestMax_Integers tests Max with integer types
func TestMax_Integers(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int
		expected int
	}{
		{"positive numbers", 5, 3, 5},
		{"negative numbers", -5, -3, -3},
		{"equal values", 7, 7, 7},
		{"zero", 0, 5, 5},
		{"negative and positive", -10, 5, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestMax_Floats tests Max with float types
func TestMax_Floats(t *testing.T) {
	tests := []struct {
		name     string
		a, b     float64
		expected float64
	}{
		{"positive floats", 3.14, 2.71, 3.14},
		{"negative floats", -5.5, -2.2, -2.2},
		{"equal floats", 1.5, 1.5, 1.5},
		{"mixed signs", -1.5, 2.5, 2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%f, %f) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestMax_Strings tests Max with string types
func TestMax_Strings(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected string
	}{
		{"alphabetical", "apple", "banana", "banana"},
		{"equal strings", "hello", "hello", "hello"},
		{"empty string", "", "test", "test"},
		{"case sensitive", "Apple", "apple", "apple"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Max(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Max(%q, %q) = %q, want %q", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestMin_Integers tests Min with integer types
func TestMin_Integers(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int
		expected int
	}{
		{"positive numbers", 5, 3, 3},
		{"negative numbers", -5, -3, -5},
		{"equal values", 7, 7, 7},
		{"zero", 0, 5, 0},
		{"negative and positive", -10, 5, -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestMin_Strings tests Min with string types
func TestMin_Strings(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected string
	}{
		{"alphabetical", "apple", "banana", "apple"},
		{"equal strings", "hello", "hello", "hello"},
		{"empty string", "", "test", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%q, %q) = %q, want %q", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestClamp_Integers tests Clamp with integer types
func TestClamp_Integers(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		min, max int
		expected int
	}{
		{"value in range", 5, 0, 10, 5},
		{"value below min", -5, 0, 10, 0},
		{"value above max", 15, 0, 10, 10},
		{"value equals min", 0, 0, 10, 0},
		{"value equals max", 10, 0, 10, 10},
		{"negative range", -5, -10, -1, -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Clamp(tt.value, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("Clamp(%d, %d, %d) = %d, want %d", tt.value, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

// TestClamp_Floats tests Clamp with float types
func TestClamp_Floats(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		min, max float64
		expected float64
	}{
		{"value in range", 5.5, 0.0, 10.0, 5.5},
		{"value below min", -5.5, 0.0, 10.0, 0.0},
		{"value above max", 15.5, 0.0, 10.0, 10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Clamp(tt.value, tt.min, tt.max)
			if result != tt.expected {
				t.Errorf("Clamp(%f, %f, %f) = %f, want %f", tt.value, tt.min, tt.max, result, tt.expected)
			}
		})
	}
}

// TestMax_Int64 tests Max with int64 type
func TestMax_Int64(t *testing.T) {
	result := Max(int64(100), int64(50))
	if result != 100 {
		t.Errorf("Max(100, 50) = %d, want 100", result)
	}
}

// TestMax_Uint tests Max with uint type
func TestMax_Uint(t *testing.T) {
	result := Max(uint(100), uint(50))
	if result != 100 {
		t.Errorf("Max(100, 50) = %d, want 100", result)
	}
}
