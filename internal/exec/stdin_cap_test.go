package exec

import (
	"bytes"
	"strings"
	"testing"
)

// TestReadAllCapped_UnderCap returns all bytes when the input is
// smaller than the limit.
func TestReadAllCapped_UnderCap(t *testing.T) {
	in := bytes.NewReader([]byte("hello"))
	got, err := readAllCapped(in, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

// TestReadAllCapped_AtCap accepts inputs at exactly the limit.
func TestReadAllCapped_AtCap(t *testing.T) {
	payload := strings.Repeat("a", 100)
	in := bytes.NewReader([]byte(payload))
	got, err := readAllCapped(in, 100)
	if err != nil {
		t.Fatalf("err at-cap: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("len = %d, want 100", len(got))
	}
}

// TestReadAllCapped_OverCap returns a UsageError suggesting --file
// instead of crashing or truncating silently. Codex round-R parity
// with claude-code 2.1.128's >10MB stdin crash fix.
func TestReadAllCapped_OverCap(t *testing.T) {
	payload := strings.Repeat("a", 101)
	in := bytes.NewReader([]byte(payload))
	_, err := readAllCapped(in, 100)
	if err == nil {
		t.Fatal("expected error on over-cap input")
	}
	var ue *UsageError
	if !asUE(err, &ue) {
		t.Fatalf("err not UsageError: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "100-byte cap") {
		t.Errorf("err should mention cap: %v", err)
	}
	if !strings.Contains(err.Error(), "--prompt-file") {
		t.Errorf("err should hint at --prompt-file: %v", err)
	}
}

// asUE is errors.As without the import.
func asUE(err error, out **UsageError) bool {
	ue, ok := err.(*UsageError)
	if ok {
		*out = ue
	}
	return ok
}
