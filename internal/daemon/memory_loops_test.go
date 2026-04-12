package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryManager_NudgeInterval(t *testing.T) {
	dir := t.TempDir()
	mm := NewMemoryManager(dir, nil)

	task := &Task{
		ID:              "task-nudge",
		TaskDescription: "fix bug",
		Status:          "merged",
	}
	events := []*TaskEvent{
		{EventType: "tool_read", Data: "{}"},
	}

	// Tasks 1 and 2 should NOT create learnings.
	mm.PostTaskReview(task, events)
	mm.PostTaskReview(task, events)
	// Allow goroutines to finish.
	time.Sleep(100 * time.Millisecond)

	memDir := filepath.Join(dir, "memory")
	if _, err := os.Stat(memDir); err == nil {
		entries, _ := os.ReadDir(memDir)
		if len(entries) > 0 {
			t.Errorf(
				"learnings created before nudge interval: %d files",
				len(entries),
			)
		}
	}

	// Task 3 should trigger the nudge (3 % 3 == 0).
	mm.PostTaskReview(task, events)
	time.Sleep(100 * time.Millisecond)

	path := filepath.Join(memDir, task.ID+"-learnings.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("learnings file not created at nudge interval")
	}
}

func TestMemoryManager_PostTaskReview_WritesLearnings(t *testing.T) {
	dir := t.TempDir()
	mm := NewMemoryManager(dir, nil)
	// Set nudgeEvery to 1 so every task triggers a review.
	mm.nudgeEvery = 1

	task := &Task{
		ID:              "task-learn",
		TaskDescription: "add endpoint",
		Status:          "merged",
	}
	events := []*TaskEvent{
		{EventType: "phase_started", Data: `{"phase":"plan"}`},
		{EventType: "tool_read", Data: `{"file":"main.go"}`},
	}

	mm.PostTaskReview(task, events)
	time.Sleep(100 * time.Millisecond)

	path := filepath.Join(dir, "memory", task.ID+"-learnings.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read learnings: %v", err)
	}
	content := string(data)
	if len(content) == 0 {
		t.Error("learnings file is empty")
	}
	// Should contain the task description.
	if !containsStr(content, "add endpoint") {
		t.Error("learnings missing task description")
	}
}

func TestCountToolCalls(t *testing.T) {
	tests := []struct {
		name   string
		events []*TaskEvent
		want   int
	}{
		{
			name:   "empty",
			events: nil,
			want:   0,
		},
		{
			name: "no tool events",
			events: []*TaskEvent{
				{EventType: "phase_started"},
				{EventType: "phase_completed"},
			},
			want: 0,
		},
		{
			name: "mixed events",
			events: []*TaskEvent{
				{EventType: "tool_read"},
				{EventType: "phase_started"},
				{EventType: "tool_write"},
				{EventType: "tool_exec"},
				{EventType: "phase_completed"},
			},
			want: 3,
		},
		{
			name: "all tool events",
			events: []*TaskEvent{
				{EventType: "tool_read"},
				{EventType: "tool_write"},
				{EventType: "tool_exec"},
				{EventType: "tool_search"},
				{EventType: "tool_replace"},
				{EventType: "tool_patch"},
			},
			want: 6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CountToolCalls(tt.events)
			if got != tt.want {
				t.Errorf(
					"CountToolCalls() = %d, want %d",
					got, tt.want,
				)
			}
		})
	}
}

func TestMaybeCreateSkill_AboveThreshold(t *testing.T) {
	dir := t.TempDir()
	mm := NewMemoryManager(dir, nil)

	task := &Task{
		ID:              "task-skill",
		TaskDescription: "implement auth module",
		Status:          "merged",
	}
	events := make([]*TaskEvent, 0, 7)
	for i := 0; i < 7; i++ {
		events = append(events, &TaskEvent{
			EventType: "tool_exec",
			Data:      "{}",
		})
	}

	mm.maybeCreateSkill(task, events)

	// Skill dir should exist with SKILL.md.
	skillDir := filepath.Join(
		dir, "skills", "implement-auth-module",
	)
	path := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if len(data) == 0 {
		t.Error("SKILL.md is empty")
	}
}

func TestMaybeCreateSkill_BelowThreshold(t *testing.T) {
	dir := t.TempDir()
	mm := NewMemoryManager(dir, nil)

	task := &Task{
		ID:              "task-noskill",
		TaskDescription: "fix typo",
		Status:          "merged",
	}
	events := []*TaskEvent{
		{EventType: "tool_read", Data: "{}"},
		{EventType: "tool_write", Data: "{}"},
	}

	mm.maybeCreateSkill(task, events)

	// No skill dir should be created.
	skillDir := filepath.Join(dir, "skills")
	if _, err := os.Stat(skillDir); err == nil {
		entries, _ := os.ReadDir(skillDir)
		if len(entries) > 0 {
			t.Errorf(
				"skill created below threshold: %d dirs",
				len(entries),
			)
		}
	}
}

// containsStr checks if s contains substr.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
