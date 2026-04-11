package agent

import (
	"sync"
	"testing"
	"time"
)

func TestMailbox_SendDrain(t *testing.T) {
	mb := NewMailbox()
	mb.Send(InterAgentMessage{From: "/root", To: "/worker", Content: "task1"})
	mb.Send(InterAgentMessage{From: "/root", To: "/worker", Content: "task2"})

	if !mb.HasPending() {
		t.Fatal("expected pending")
	}
	msgs := mb.Drain()
	if len(msgs) != 2 {
		t.Fatalf("got %d, want 2", len(msgs))
	}
	if msgs[0].Content != "task1" || msgs[1].Content != "task2" {
		t.Errorf("wrong order: %v", msgs)
	}
	if msgs[0].SeqNo >= msgs[1].SeqNo {
		t.Error("seq should be monotonic")
	}
	if mb.HasPending() {
		t.Error("should be empty after drain")
	}
}

func TestMailbox_Subscribe(t *testing.T) {
	mb := NewMailbox()
	ch := mb.Subscribe()

	go func() {
		time.Sleep(50 * time.Millisecond)
		mb.Send(InterAgentMessage{Content: "wake"})
	}()

	select {
	case <-ch:
		// good — notified
	case <-time.After(2 * time.Second):
		t.Fatal("subscribe not notified")
	}
}

func TestMailbox_TriggerTurn(t *testing.T) {
	mb := NewMailbox()
	mb.Send(InterAgentMessage{Content: "no-trigger"})
	if mb.HasTriggerTurn() {
		t.Error("no trigger expected")
	}
	mb.Send(InterAgentMessage{Content: "trigger", TriggerTurn: true})
	if !mb.HasTriggerTurn() {
		t.Error("trigger expected")
	}
}

func TestMailbox_ConcurrentSendDrain(t *testing.T) {
	mb := NewMailbox()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mb.Send(InterAgentMessage{Content: "msg"})
		}()
	}
	wg.Wait()
	msgs := mb.Drain()
	if len(msgs) != 100 {
		t.Fatalf("got %d, want 100", len(msgs))
	}
}

// TestMailbox_DropOldestOnOverflow exercises the bounded-queue eviction
// path. Without a cap a crashed/paused recipient would let the queue
// grow unbounded; verify drop-oldest semantics keep the most recent
// messages and bump the dropped counter.
func TestMailbox_DropOldestOnOverflow(t *testing.T) {
	mb := NewMailboxWithCapacity(5)
	for i := 0; i < 10; i++ {
		mb.Send(InterAgentMessage{Content: "msg"})
	}
	msgs := mb.Drain()
	if len(msgs) != 5 {
		t.Fatalf("len = %d, want 5", len(msgs))
	}
	if mb.Dropped() != 5 {
		t.Errorf("dropped = %d, want 5", mb.Dropped())
	}
	// The last 5 sequence numbers (6..10) should be the survivors
	// because drop-oldest keeps the newest entries.
	if msgs[0].SeqNo != 6 {
		t.Errorf("first surviving seq = %d, want 6", msgs[0].SeqNo)
	}
	if msgs[4].SeqNo != 10 {
		t.Errorf("last surviving seq = %d, want 10", msgs[4].SeqNo)
	}
}

// TestMailbox_DefaultCapacity verifies NewMailbox picks up the
// package default and that NewMailboxWithCapacity(0) falls back to it.
func TestMailbox_DefaultCapacity(t *testing.T) {
	mb := NewMailboxWithCapacity(0)
	if mb.maxSize != defaultMailboxCap {
		t.Errorf("maxSize = %d, want defaultMailboxCap %d", mb.maxSize, defaultMailboxCap)
	}
}
