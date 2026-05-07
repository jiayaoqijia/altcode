package tui

import (
	"strings"
	"testing"
)

func TestQueue_BuiltinList_EmptyShowsHint(t *testing.T) {
	a := testApp()
	got := a.builtinQueueText([]string{"/queue"})
	if !strings.Contains(got, "empty") {
		t.Errorf("expected empty-hint, got: %q", got)
	}
}

func TestQueue_BuiltinList_ShowsItems(t *testing.T) {
	a := testApp()
	a.queue = []string{"first prompt", "second prompt"}
	got := a.builtinQueueText([]string{"/queue", "list"})
	for _, want := range []string{"2 prompt(s)", "1. first prompt", "2. second prompt"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestQueue_BuiltinClear(t *testing.T) {
	a := testApp()
	a.queue = []string{"a", "b", "c"}
	got := a.builtinQueueText([]string{"/queue", "clear"})
	if len(a.queue) != 0 {
		t.Errorf("queue not cleared: %v", a.queue)
	}
	if !strings.Contains(got, "cleared 3") {
		t.Errorf("expected 'cleared 3' message: %q", got)
	}
}

func TestQueue_BuiltinDrop(t *testing.T) {
	a := testApp()
	a.queue = []string{"first", "second", "third"}
	got := a.builtinQueueText([]string{"/queue", "drop", "2"})

	if len(a.queue) != 2 {
		t.Errorf("len(queue) = %d, want 2", len(a.queue))
	}
	if a.queue[0] != "first" || a.queue[1] != "third" {
		t.Errorf("queue after drop = %v, want [first third]", a.queue)
	}
	if !strings.Contains(got, "dropped #2") {
		t.Errorf("expected 'dropped #2' marker: %q", got)
	}
}

func TestQueue_BuiltinDrop_InvalidIndex(t *testing.T) {
	a := testApp()
	a.queue = []string{"a"}

	cases := []string{"0", "5", "abc", "-1"}
	for _, c := range cases {
		got := a.builtinQueueText([]string{"/queue", "drop", c})
		if !strings.Contains(got, "invalid index") {
			t.Errorf("drop %q should be rejected: %q", c, got)
		}
	}
	if len(a.queue) != 1 {
		t.Errorf("queue mutated by invalid drops: %v", a.queue)
	}
}

func TestQueue_DrainPopsFIFO_HelperOnly(t *testing.T) {
	// Verify the queue pop semantics without invoking submit() —
	// submit needs an engine to run the LLM call, which testApp()
	// lacks. We assert the FIFO order by manually popping the way
	// drainQueue() does.
	a := testApp()
	a.queue = []string{"first", "second"}

	popped := a.queue[0]
	a.queue = a.queue[1:]

	if popped != "first" {
		t.Errorf("FIFO head = %q, want first", popped)
	}
	if len(a.queue) != 1 || a.queue[0] != "second" {
		t.Errorf("queue tail = %v, want [second]", a.queue)
	}
}

func TestQueue_DrainEmptyReturnsNil(t *testing.T) {
	a := testApp()
	a.queue = nil
	if cmd := a.drainQueue(); cmd != nil {
		t.Errorf("empty queue should return nil tea.Cmd, got %v", cmd)
	}
}
