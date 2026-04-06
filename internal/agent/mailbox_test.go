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
