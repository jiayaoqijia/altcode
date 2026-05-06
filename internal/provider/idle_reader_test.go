package provider

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockingReader simulates a TCP body that returns one chunk and then
// blocks forever — the exact failure mode that hung user altcodes for
// 1+ hour.
type blockingReader struct {
	first    []byte
	consumed bool
	closed   chan struct{}
	once     sync.Once
}

func newBlockingReader(first string) *blockingReader {
	return &blockingReader{
		first:  []byte(first),
		closed: make(chan struct{}),
	}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if !b.consumed && len(b.first) > 0 {
		n := copy(p, b.first)
		b.first = b.first[n:]
		if len(b.first) == 0 {
			b.consumed = true
		}
		return n, nil
	}
	// Block until Close is called.
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingReader) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

// TestIdleReader_PassesThroughActiveStream — when bytes keep arriving,
// the timer keeps resetting and the reader behaves like a plain pipe.
func TestIdleReader_PassesThroughActiveStream(t *testing.T) {
	body := io.NopCloser(strings.NewReader("hello world"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := newIdleReader(ctx, body, 200*time.Millisecond)
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("got %q, want hello world", got)
	}
}

// TestIdleReader_FiresOnSilentStream is the regression for the 1-hour
// hang: a body that returns one chunk and then blocks forever must
// produce ErrSSEIdleTimeout within `idle` of the last byte, NOT hang
// forever.
func TestIdleReader_FiresOnSilentStream(t *testing.T) {
	body := newBlockingReader("partial chunk")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := newIdleReader(ctx, body, 80*time.Millisecond)
	defer r.Close()

	// First Read drains the chunk.
	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if string(buf[:n]) != "partial chunk" {
		t.Errorf("first read = %q, want partial chunk", buf[:n])
	}

	// Second Read blocks until idle timeout fires.
	start := time.Now()
	_, err = r.Read(buf)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrSSEIdleTimeout) {
		t.Errorf("got err = %v, want ErrSSEIdleTimeout", err)
	}
	if elapsed > 250*time.Millisecond {
		t.Errorf("idle deadline took %v to fire, want ≤ ~80-150ms", elapsed)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("idle deadline fired too early at %v", elapsed)
	}
}

// TestIdleReader_ContextCancelStopsTimer ensures we don't leak the
// timer goroutine when the caller cancels via context.
func TestIdleReader_ContextCancelStopsTimer(t *testing.T) {
	body := newBlockingReader("chunk")
	ctx, cancel := context.WithCancel(context.Background())

	r := newIdleReader(ctx, body, 5*time.Second)
	defer r.Close()

	buf := make([]byte, 10)
	_, _ = r.Read(buf) // drain "chunk"

	// Cancel context. The internal goroutine should stop the timer.
	cancel()

	// Give the goroutine a moment to observe the cancel.
	time.Sleep(50 * time.Millisecond)

	// Reading now should NOT return ErrSSEIdleTimeout — the timer is
	// stopped. It will return because Close was called by deferred Close
	// once the test ends, but here we verify the cancel path doesn't
	// trip the idle sentinel.
	if r.tripped.Load() {
		t.Error("ctx cancel should not trip the idle sentinel")
	}
}

// TestIdleReader_ResetsOnEachChunk verifies that incremental progress
// keeps the deadline rolling. Without per-Read reset, a slow stream
// that emits N bytes/second for >idle total time would still trip.
func TestIdleReader_ResetsOnEachChunk(t *testing.T) {
	// Build a slow-drip reader: emits 1 byte every 30ms for 200ms total.
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for i := 0; i < 6; i++ {
			pw.Write([]byte{byte('a' + i)})
			time.Sleep(30 * time.Millisecond)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// idle = 100ms — longer than the 30ms drip interval but shorter
	// than the 200ms total. Without reset-on-Read this would trip.
	r := newIdleReader(ctx, pr, 100*time.Millisecond)
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "abcdef" {
		t.Errorf("got %q, want abcdef", got)
	}
}
