package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleState() *TaskState {
	return &TaskState{
		TaskID: "task-abc-123",
		Phase:  "implement",
		Plan: &Plan{
			Steps: []PlanStep{
				{Description: "step 1", Prompt: "do step 1"},
				{Description: "step 2", Prompt: "do step 2"},
			},
		},
		Progress: []StepResult{
			{StepIndex: 0, Status: "done", Output: "ok", Attempts: 1},
		},
		GitState: GitState{
			Branch:     "feat/task-abc",
			LastCommit: "deadbeef",
			Worktree:   "/tmp/wt",
		},
		Decisions: []string{"use retry", "skip lint"},
	}
}

func TestSaveLoadTaskState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	orig := sampleState()

	if err := SaveTaskState(dir, orig); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	loaded, err := LoadTaskState(dir, orig.TaskID)
	if err != nil {
		t.Fatalf("LoadTaskState: %v", err)
	}

	// Verify key fields round-trip.
	if loaded.TaskID != orig.TaskID {
		t.Errorf("TaskID: got %q, want %q", loaded.TaskID, orig.TaskID)
	}
	if loaded.Phase != orig.Phase {
		t.Errorf("Phase: got %q, want %q", loaded.Phase, orig.Phase)
	}
	if loaded.Version != taskStateVersion {
		t.Errorf("Version: got %d, want %d", loaded.Version, taskStateVersion)
	}
	if loaded.Checksum == "" {
		t.Error("Checksum should be non-empty")
	}
	if len(loaded.Plan.Steps) != 2 {
		t.Errorf("Plan steps: got %d, want 2", len(loaded.Plan.Steps))
	}
	if len(loaded.Progress) != 1 {
		t.Errorf("Progress: got %d, want 1", len(loaded.Progress))
	}
	if loaded.Progress[0].Status != "done" {
		t.Errorf("Progress[0].Status: got %q, want %q",
			loaded.Progress[0].Status, "done")
	}
	if loaded.GitState.Branch != "feat/task-abc" {
		t.Errorf("GitState.Branch: got %q", loaded.GitState.Branch)
	}
	if len(loaded.Decisions) != 2 {
		t.Errorf("Decisions: got %d, want 2", len(loaded.Decisions))
	}
	if loaded.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestSaveTaskState_Checksum(t *testing.T) {
	dir := t.TempDir()
	state := sampleState()

	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	// Corrupt the file on disk.
	path := filepath.Join(dir, state.TaskID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	corrupted := strings.Replace(
		string(data), `"implement"`, `"review"`, 1,
	)
	if err := os.WriteFile(path, []byte(corrupted), 0o644); err != nil {
		t.Fatalf("write corrupted: %v", err)
	}

	_, err = LoadTaskState(dir, state.TaskID)
	if !errors.Is(err, ErrCorruptArtifact) {
		t.Errorf("expected ErrCorruptArtifact, got: %v", err)
	}
}

func TestLoadTaskState_UnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	state := sampleState()

	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	// Patch the version to something unsupported.
	path := filepath.Join(dir, state.TaskID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	patched := strings.Replace(
		string(data), `"version": 1`, `"version": 99`, 1,
	)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatalf("write patched: %v", err)
	}

	_, err = LoadTaskState(dir, state.TaskID)
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Errorf("expected ErrUnsupportedVersion, got: %v", err)
	}
}

func TestSaveTaskState_TooLarge(t *testing.T) {
	dir := t.TempDir()
	state := sampleState()

	// Fill Decisions with enough data to exceed maxArtifactBytes.
	big := strings.Repeat("x", 1024)
	state.Decisions = make([]string, 1200)
	for i := range state.Decisions {
		state.Decisions[i] = big
	}

	err := SaveTaskState(dir, state)
	if err == nil {
		t.Fatal("expected error for oversized state")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSaveTaskState_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	state := sampleState()

	if err := SaveTaskState(dir, state); err != nil {
		t.Fatalf("SaveTaskState: %v", err)
	}

	// The .tmp file should not exist after a successful write.
	tmp := filepath.Join(dir, state.TaskID, "state.json.tmp")
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file should not exist after save, got err: %v", err)
	}

	// The final file should exist.
	final := filepath.Join(dir, state.TaskID, "state.json")
	if _, err := os.Stat(final); err != nil {
		t.Errorf("state.json should exist: %v", err)
	}
}

func TestLoadTaskState_FileNotFound(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadTaskState(dir, "nonexistent-task")
	if !os.IsNotExist(err) {
		t.Errorf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestBuildAgentContext_Lead(t *testing.T) {
	state := sampleState()
	ctx := BuildAgentContext(state, "lead")

	for _, want := range []string{
		"Task: task-abc-123",
		"Phase: implement",
		"Plan:",
		"Progress:",
		"Decisions:",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("lead context missing %q in:\n%s", want, ctx)
		}
	}
}

func TestBuildAgentContext_Reviewer(t *testing.T) {
	state := sampleState()
	ctx := BuildAgentContext(state, "reviewer")

	// Reviewer should see plan and decisions.
	for _, want := range []string{
		"Review the changes",
		"Plan:",
		"Decisions so far:",
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("reviewer context missing %q in:\n%s", want, ctx)
		}
	}

	// Reviewer should NOT see progress details (implementation output).
	if strings.Contains(ctx, "Progress:") {
		t.Errorf("reviewer should not see Progress in:\n%s", ctx)
	}
}
