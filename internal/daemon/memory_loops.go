package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemoryManager handles post-task learning. It runs non-blocking
// background loops after task completion to extract learnings and
// detect reusable skill patterns.
type MemoryManager struct {
	dataDir    string
	nudgeEvery int // run review after every N tasks
	taskCount  int
	mu         sync.Mutex
	logger     *slog.Logger
}

// NewMemoryManager creates a MemoryManager that stores artifacts
// under dataDir. nudgeEvery defaults to 3 if <= 0.
func NewMemoryManager(
	dataDir string, logger *slog.Logger,
) *MemoryManager {
	if logger == nil {
		logger = slog.New(
			slog.NewJSONHandler(os.Stderr, nil),
		)
	}
	return &MemoryManager{
		dataDir:    dataDir,
		nudgeEvery: 3,
		logger:     logger,
	}
}

// PostTaskReview runs after a task completes. Checks if it is
// time for a memory nudge and always checks for skill creation.
// Both are non-blocking -- errors are logged, not returned.
func (m *MemoryManager) PostTaskReview(
	task *Task, events []*TaskEvent,
) {
	m.mu.Lock()
	m.taskCount++
	count := m.taskCount
	m.mu.Unlock()

	if count%m.nudgeEvery == 0 {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					m.logger.Error("panic in review nudge", "task", task.ID, "panic", r)
				}
			}()
			m.backgroundReviewNudge(task, events)
		}()
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("panic in skill creation", "task", task.ID, "panic", r)
			}
		}()
		m.maybeCreateSkill(task, events)
	}()
}

// TaskCount returns the current task count (thread-safe).
func (m *MemoryManager) TaskCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.taskCount
}

// backgroundReviewNudge extracts learnings from the task and
// writes them to {dataDir}/memory/{taskID}-learnings.md.
// Uses atomic write (temp + rename).
func (m *MemoryManager) backgroundReviewNudge(
	task *Task, events []*TaskEvent,
) {
	dir := filepath.Join(m.dataDir, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.logger.Error("mkdir memory",
			"err", err, "dir", dir)
		return
	}

	content := buildLearnings(task, events)
	path := filepath.Join(dir, task.ID+"-learnings.md")
	if err := atomicWrite(path, content); err != nil {
		m.logger.Error("write learnings",
			"err", err, "task", task.ID)
	}
}

// maybeCreateSkill checks if the task pattern is reusable.
// A task with > 5 tool calls is considered potentially reusable.
func (m *MemoryManager) maybeCreateSkill(
	task *Task, events []*TaskEvent,
) {
	if CountToolCalls(events) <= 5 {
		return
	}

	name := skillName(task.TaskDescription)
	dir := filepath.Join(m.dataDir, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.logger.Error("mkdir skill",
			"err", err, "dir", dir)
		return
	}

	content := buildSkillMD(task, events)
	path := filepath.Join(dir, "SKILL.md")
	if err := atomicWrite(path, content); err != nil {
		m.logger.Error("write skill",
			"err", err, "skill", name)
	}
}

// CountToolCalls counts events whose EventType starts with
// "tool_" in the event list.
func CountToolCalls(events []*TaskEvent) int {
	n := 0
	for _, e := range events {
		if strings.HasPrefix(e.EventType, "tool_") {
			n++
		}
	}
	return n
}

// buildLearnings produces a markdown summary of the task.
func buildLearnings(
	task *Task, events []*TaskEvent,
) string {
	var b strings.Builder
	b.WriteString("# Learnings: " + task.ID + "\n\n")
	b.WriteString("## Task\n\n")
	b.WriteString(task.TaskDescription + "\n\n")
	b.WriteString("## Status\n\n")
	b.WriteString(task.Status + "\n\n")
	b.WriteString("## Events\n\n")
	for _, e := range events {
		b.WriteString(fmt.Sprintf(
			"- [%s] %s\n", e.EventType, e.Data,
		))
	}
	return b.String()
}

// buildSkillMD produces a SKILL.md from the task pattern.
func buildSkillMD(
	task *Task, events []*TaskEvent,
) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: >-\n")
	b.WriteString("  Auto-detected reusable pattern from task.\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + skillName(task.TaskDescription) + "\n\n")
	b.WriteString("Pattern extracted from: " +
		task.TaskDescription + "\n\n")
	b.WriteString("## Tool calls\n\n")
	for _, e := range events {
		if strings.HasPrefix(e.EventType, "tool_") {
			b.WriteString("- " + e.EventType + "\n")
		}
	}
	return b.String()
}

// skillName derives a kebab-case name from a task description.
func skillName(desc string) string {
	words := strings.Fields(strings.ToLower(desc))
	if len(words) > 4 {
		words = words[:4]
	}
	name := strings.Join(words, "-")
	// Strip non-alphanumeric except hyphens.
	var clean strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') || r == '-' {
			clean.WriteRune(r)
		}
	}
	result := clean.String()
	if result == "" {
		return "unnamed-skill"
	}
	return result
}

// atomicWrite writes data to path via temp file + rename.
func atomicWrite(path, data string) error {
	tmp := path + ".tmp." +
		fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(
		tmp, []byte(data), 0o644,
	); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
