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
	if !errors.Is(err, errors.New("division by zero")) {
		t.Errorf("expected 'division by zero' error, got %v", err)
	}
	if result != 0 {
		t.Errorf("expected 0 on error, got %d", result)
	}
}

func TestDiv_NegativeDividend(t *testing.T) {
	result, err := Div(-10, 2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != -5 {
		t.Errorf("expected -5, got %d", result)
	}
}

func TestDiv_NegativeDivisor(t *testing.T) {
	result, err := Div(10, -2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != -5 {
		t.Errorf("expected -5, got %d", result)
	}
}

func TestDiv_BothNegative(t *testing.T) {
	result, err := Div(-10, -2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 5 {
		t.Errorf("expected 5, got %d", result)
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

func TestDiv_ResultWithRemainder(t *testing.T) {
	result, err := Div(7, 2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 3 {
		t.Errorf("expected 3 (integer division), got %d", result)
	}
}
