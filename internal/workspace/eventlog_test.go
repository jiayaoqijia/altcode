package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventLogEmitAndGetAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	log := NewEventLog(path)

	ev1 := Event{
		Type:    EventAgentSpawned,
		Role:    "architect",
		Content: "spawned claude",
	}
	ev2 := Event{
		Type:    EventAgentOutput,
		Role:    "implementer",
		Content: "wrote auth.go",
	}

	if err := log.Emit(ev1); err != nil {
		t.Fatalf("emit ev1: %v", err)
	}
	if err := log.Emit(ev2); err != nil {
		t.Fatalf("emit ev2: %v", err)
	}

	events, err := log.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != EventAgentSpawned {
		t.Errorf("event[0] type = %q, want %q",
			events[0].Type, EventAgentSpawned)
	}
	if events[0].ID == "" {
		t.Error("event[0] should have auto-generated ID")
	}
	if events[1].Role != "implementer" {
		t.Errorf("event[1] role = %q, want implementer",
			events[1].Role)
	}
}

func TestEventLogGetEventsSince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	log := NewEventLog(path)

	t0 := time.Now().UTC()
	ev := Event{
		Type:      EventAgentExited,
		Role:      "reviewer",
		Content:   "exit 0",
		Timestamp: t0.Add(time.Second),
	}
	if err := log.Emit(ev); err != nil {
		t.Fatalf("emit: %v", err)
	}

	got, err := log.GetEvents(t0)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event since t0, got %d", len(got))
	}

	got2, err := log.GetEvents(t0.Add(2 * time.Second))
	if err != nil {
		t.Fatalf("GetEvents future: %v", err)
	}
	if len(got2) != 0 {
		t.Errorf("expected 0 events in future, got %d",
			len(got2))
	}
}

func TestEventLogTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	log := NewEventLog(path)

	for i := 0; i < 5; i++ {
		if err := log.Emit(Event{
			Type:    EventToolCall,
			Content: "call",
		}); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}

	got, err := log.Tail(3)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 tail events, got %d", len(got))
	}

	all, err := log.Tail(100)
	if err != nil {
		t.Fatalf("Tail(100): %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("expected 5 events for large tail, got %d",
			len(all))
	}
}

func TestEventLogEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	log := NewEventLog(path)

	events, err := log.GetAll()
	if err != nil {
		t.Fatalf("GetAll on missing file: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}

	// Create empty file
	os.WriteFile(path, nil, 0o644)
	events, err = log.GetAll()
	if err != nil {
		t.Fatalf("GetAll on empty file: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events from empty file, got %d",
			len(events))
	}
}
