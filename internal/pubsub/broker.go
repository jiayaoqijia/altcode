// Package pubsub provides a generic, typed in-process pub/sub broker
// for reactive TUI updates.
package pubsub

import (
	"context"
	"sync"
)

// EventType identifies the kind of event.
type EventType string

const (
	Created EventType = "created"
	Updated EventType = "updated"
	Deleted EventType = "deleted"
)

// Event carries a typed payload with an event classification.
type Event[T any] struct {
	Type    EventType
	Payload T
}

const defaultBufSize = 64

// Broker fans out events to all active subscribers.
type Broker[T any] struct {
	subs map[chan Event[T]]struct{}
	mu   sync.RWMutex
	done chan struct{}
}

// NewBroker creates a ready-to-use Broker.
func NewBroker[T any]() *Broker[T] {
	return &Broker[T]{
		subs: make(map[chan Event[T]]struct{}),
		done: make(chan struct{}),
	}
}

// Subscribe returns a channel that receives events until ctx is
// cancelled or the broker is shut down. bufSize controls the channel
// buffer (0 uses the default of 64).
func (b *Broker[T]) Subscribe(
	ctx context.Context, bufSize int,
) <-chan Event[T] {
	if bufSize <= 0 {
		bufSize = defaultBufSize
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// If already shut down, return a closed channel.
	select {
	case <-b.done:
		ch := make(chan Event[T])
		close(ch)
		return ch
	default:
	}

	sub := make(chan Event[T], bufSize)
	b.subs[sub] = struct{}{}

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()

		select {
		case <-b.done:
			return
		default:
		}
		delete(b.subs, sub)
		close(sub)
	}()

	return sub
}

// Publish sends an event to every active subscriber. Non-blocking:
// slow subscribers with a full buffer will miss events.
func (b *Broker[T]) Publish(event Event[T]) {
	b.mu.RLock()
	select {
	case <-b.done:
		b.mu.RUnlock()
		return
	default:
	}

	// Snapshot subscribers so we release the lock fast.
	subs := make([]chan Event[T], 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default: // drop if full
		}
	}
}

// Shutdown closes all subscriber channels and prevents new
// subscriptions. Safe to call multiple times.
func (b *Broker[T]) Shutdown() {
	select {
	case <-b.done:
		return
	default:
		close(b.done)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}
}

// SubscriberCount returns the current number of active subscribers.
func (b *Broker[T]) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
