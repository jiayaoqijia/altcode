package pubsub

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewBroker(t *testing.T) {
	b := NewBroker[string]()
	if b == nil {
		t.Fatal("NewBroker returned nil")
	}
	if b.SubscriberCount() != 0 {
		t.Errorf("new broker should have 0 subs, got %d", b.SubscriberCount())
	}
}

func TestSubscribeAndPublish(t *testing.T) {
	b := NewBroker[string]()
	defer b.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, 0)
	if b.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber, got %d", b.SubscriberCount())
	}

	b.Publish(Event[string]{Type: Created, Payload: "hello"})

	select {
	case ev := <-ch:
		if ev.Type != Created {
			t.Errorf("type = %q, want %q", ev.Type, Created)
		}
		if ev.Payload != "hello" {
			t.Errorf("payload = %q, want %q", ev.Payload, "hello")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := NewBroker[int]()
	defer b.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1 := b.Subscribe(ctx, 0)
	ch2 := b.Subscribe(ctx, 0)

	if b.SubscriberCount() != 2 {
		t.Fatalf("expected 2 subs, got %d", b.SubscriberCount())
	}

	b.Publish(Event[int]{Type: Updated, Payload: 42})

	for i, ch := range []<-chan Event[int]{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Payload != 42 {
				t.Errorf("sub%d payload = %d, want 42", i, ev.Payload)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub%d timed out", i)
		}
	}
}

func TestContextCancellationUnsubscribes(t *testing.T) {
	b := NewBroker[string]()
	defer b.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	ch := b.Subscribe(ctx, 0)
	if b.SubscriberCount() != 1 {
		t.Fatalf("expected 1 sub, got %d", b.SubscriberCount())
	}

	cancel()
	// Give the goroutine time to clean up.
	time.Sleep(50 * time.Millisecond)

	if b.SubscriberCount() != 0 {
		t.Errorf("after cancel: expected 0 subs, got %d", b.SubscriberCount())
	}

	// Channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after cancel")
	}
}

func TestShutdownClosesChannels(t *testing.T) {
	b := NewBroker[string]()
	ctx := context.Background()
	ch := b.Subscribe(ctx, 0)

	b.Shutdown()

	_, ok := <-ch
	if ok {
		t.Error("expected channel closed after shutdown")
	}

	if b.SubscriberCount() != 0 {
		t.Errorf("after shutdown: subs = %d", b.SubscriberCount())
	}
}

func TestShutdownIdempotent(t *testing.T) {
	b := NewBroker[string]()
	b.Shutdown()
	b.Shutdown() // should not panic
}

func TestPublishAfterShutdown(t *testing.T) {
	b := NewBroker[string]()
	b.Shutdown()
	// Should not panic.
	b.Publish(Event[string]{Type: Created, Payload: "noop"})
}

func TestSubscribeAfterShutdown(t *testing.T) {
	b := NewBroker[string]()
	b.Shutdown()

	ctx := context.Background()
	ch := b.Subscribe(ctx, 0)

	// Returned channel should be immediately closed.
	_, ok := <-ch
	if ok {
		t.Error("subscribe after shutdown should return closed chan")
	}
}

func TestDropsOnFullBuffer(t *testing.T) {
	b := NewBroker[int]()
	defer b.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, 1) // tiny buffer

	// Publish more events than the buffer can hold.
	for i := 0; i < 10; i++ {
		b.Publish(Event[int]{Type: Updated, Payload: i})
	}

	// Should get at least 1, but not panic.
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected at least one event")
	}
}

func TestEventTypes(t *testing.T) {
	if Created != "created" {
		t.Errorf("Created = %q", Created)
	}
	if Updated != "updated" {
		t.Errorf("Updated = %q", Updated)
	}
	if Deleted != "deleted" {
		t.Errorf("Deleted = %q", Deleted)
	}
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	b := NewBroker[int]()
	defer b.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Spawn subscribers.
	for i := 0; i < 5; i++ {
		ch := b.Subscribe(ctx, 64)
		wg.Add(1)
		go func() {
			defer wg.Done()
			count := 0
			for range ch {
				count++
				if count >= 10 {
					return
				}
			}
		}()
	}

	// Publish concurrently.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				b.Publish(Event[int]{
					Type:    Updated,
					Payload: j,
				})
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent test timed out")
	}
}

func TestCustomBufferSize(t *testing.T) {
	b := NewBroker[string]()
	defer b.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, 128)

	// Fill beyond default buffer (64) to confirm custom size works.
	for i := 0; i < 100; i++ {
		b.Publish(Event[string]{Type: Created, Payload: "x"})
	}

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count < 100 {
		t.Errorf("with bufSize=128, expected 100 events, got %d", count)
	}
}

func TestStructPayload(t *testing.T) {
	type Msg struct {
		ID   int
		Text string
	}

	b := NewBroker[Msg]()
	defer b.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx, 0)
	b.Publish(Event[Msg]{
		Type:    Created,
		Payload: Msg{ID: 1, Text: "hi"},
	})

	select {
	case ev := <-ch:
		if ev.Payload.ID != 1 || ev.Payload.Text != "hi" {
			t.Errorf("unexpected payload: %+v", ev.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
