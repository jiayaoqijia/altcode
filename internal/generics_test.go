package internal

import "testing"

func TestMax_Integers(t *testing.T) {
	result := Max(5, 10)
	if result != 10 {
		t.Errorf("Max(5, 10) = %d, want 10", result)
	}
}

func TestMax_Floats(t *testing.T) {
	result := Max(3.14, 2.71)
	if result != 3.14 {
		t.Errorf("Max(3.14, 2.71) = %f, want 3.14", result)
	}
}

func TestMax_Strings(t *testing.T) {
	result := Max("apple", "banana")
	if result != "banana" {
		t.Errorf("Max(apple, banana) = %s, want banana", result)
	}
}

func TestMax_EqualValues(t *testing.T) {
	result := Max(7, 7)
	if result != 7 {
		t.Errorf("Max(7, 7) = %d, want 7", result)
	}
}

func TestMax_NegativeNumbers(t *testing.T) {
	result := Max(-5, -10)
	if result != -5 {
		t.Errorf("Max(-5, -10) = %d, want -5", result)
	}
}

func TestMin_Integers(t *testing.T) {
	result := Min(5, 10)
	if result != 5 {
		t.Errorf("Min(5, 10) = %d, want 5", result)
	}
}

func TestMin_Floats(t *testing.T) {
	result := Min(3.14, 2.71)
	if result != 2.71 {
		t.Errorf("Min(3.14, 2.71) = %f, want 2.71", result)
	}
}

func TestMin_Strings(t *testing.T) {
	result := Min("apple", "banana")
	if result != "apple" {
		t.Errorf("Min(apple, banana) = %s, want apple", result)
	}
}
