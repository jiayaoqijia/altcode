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
	subs     map[chan Event[T]]struct{}
	mu       sync.RWMutex
	done     chan struct{}
	shutOnce sync.Once
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
//
// Holds the read lock for the duration of the fan-out so a concurrent
// Subscribe cleanup or Shutdown can't close a channel between the
// snapshot and the send. The previous version snapshotted under
// RLock then released the lock before sending — that left a race
// window where Shutdown closed the channel and the Publish goroutine
// then sent to it, panicking with 'send on closed channel'. Holding
// RLock through the send blocks Shutdown's Lock until we're done,
// which is the right tradeoff: Shutdown is rare, Publish is hot.
func (b *Broker[T]) Publish(event Event[T]) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	select {
	case <-b.done:
		return
	default:
	}
	for ch := range b.subs {
		select {
		case ch <- event:
		default: // drop if full
		}
	}
}

// Shutdown closes all subscriber channels and prevents new
// subscriptions. Safe to call multiple times — sync.Once guards the
// done close + subscriber close so concurrent Shutdown calls don't
// race on close(b.done) or double-close any subscriber channel.
func (b *Broker[T]) Shutdown() {
	b.shutOnce.Do(func() {
		// Close done first under the lock so the Subscribe cleanup
		// goroutines that wake on ctx.Done() see done as already
		// closed and skip their own delete + close (which would
		// otherwise race with our cleanup below).
		b.mu.Lock()
		defer b.mu.Unlock()
		close(b.done)
		for ch := range b.subs {
			delete(b.subs, ch)
			close(ch)
		}
	})
}

// SubscriberCount returns the current number of active subscribers.
func (b *Broker[T]) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
