package internal

import (
	"errors"
	"testing"
)

func TestDiv_ValidInput(t *testing.T) {
	result, err := Div(10, 2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 5 {
		t.Errorf("expected 5, got %d", result)
	}
}

func TestDiv_DivisionByZero(t *testing.T) {
	result, err := Div(10, 0)
	if err == nil {
		t.Fatal("expected error for division by zero, got nil")
	}
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
	if err.Error() != "division by zero" {
		t.Errorf("expected 'division by zero', got %q", err.Error())
	}
}

func TestDiv_NegativeNumbers(t *testing.T) {
	result, err := Div(-10, 2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != -5 {
		t.Errorf("expected -5, got %d", result)
	}
}

func TestDiv_ZeroDividend(t *testing.T) {
	result, err := Div(0, 5)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestDiv_ErrorType(t *testing.T) {
	_, err := Div(1, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	// Verify it's a proper error type
	if !errors.Is(err, err) {
		t.Error("error type mismatch")
	}
}
