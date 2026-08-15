package tool

import (
	"context"
	"testing"
	"time"
)

func TestPatchCommandContextAddsDeadlineWhenMissing(t *testing.T) {
	ctx, cancel := patchCommandContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("patch command context should add a deadline")
	}
	if time.Until(deadline) <= 0 {
		t.Fatalf("deadline should be in the future, got %v", deadline)
	}
}

func TestPatchCommandContextPreservesExistingDeadline(t *testing.T) {
	want := time.Now().Add(5 * time.Second)
	parent, parentCancel := context.WithDeadline(context.Background(), want)
	defer parentCancel()

	ctx, cancel := patchCommandContext(parent)
	defer cancel()

	got, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline to remain set")
	}
	if !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v", got, want)
	}
}
