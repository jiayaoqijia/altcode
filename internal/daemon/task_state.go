package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const taskStateVersion = 1
const maxArtifactBytes = 1 * 1024 * 1024 // 1 MB cap

// ErrCorruptArtifact is returned when the checksum does not match.
var ErrCorruptArtifact = errors.New("corrupt task state artifact")

// ErrUnsupportedVersion is returned for unknown version numbers.
var ErrUnsupportedVersion = errors.New("unsupported task state version")

// TaskState is the versioned artifact saved between phases for
// crash recovery and context handoff between agents.
type TaskState struct {
	Version   int          `json:"version"`
	Checksum  string       `json:"checksum"`
	TaskID    string       `json:"task_id"`
	Phase     string       `json:"phase"`
	Plan      *Plan        `json:"plan,omitempty"`
	Progress  []StepResult `json:"progress,omitempty"`
	GitState  GitState     `json:"git_state"`
	Decisions []string     `json:"decisions"`
	CreatedAt time.Time    `json:"created_at"`
}

// StepResult records the outcome of a single plan step.
type StepResult struct {
	StepIndex int    `json:"step_index"`
	Status    string `json:"status"` // done|failed|skipped
	Output    string `json:"output,omitempty"`
	Attempts  int    `json:"attempts"`
}

// GitState captures the repo state at save time.
type GitState struct {
	Branch     string `json:"branch"`
	LastCommit string `json:"last_commit"`
	Worktree   string `json:"worktree"`
}

// SaveTaskState writes the artifact to dir/{task_id}/state.json
// atomically. Computes SHA-256 checksum over content fields.
// Rejects if serialized size exceeds maxArtifactBytes.
func SaveTaskState(dir string, state *TaskState) error {
	state.Version = taskStateVersion
	state.CreatedAt = time.Now().UTC()

	// Compute checksum over content (excluding checksum field).
	state.Checksum = ""
	content, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal for checksum: %w", err)
	}

	h := sha256.Sum256(content)
	state.Checksum = hex.EncodeToString(h[:])

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task state: %w", err)
	}

	if len(data) > maxArtifactBytes {
		return fmt.Errorf(
			"task state too large: %d bytes (max %d)",
			len(data), maxArtifactBytes,
		)
	}

	// Atomic write: temp file + rename.
	stateDir := filepath.Join(dir, state.TaskID)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", stateDir, err)
	}
	path := filepath.Join(stateDir, "state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// LoadTaskState reads and validates the artifact from disk.
// Returns ErrCorruptArtifact if the checksum does not match.
// Returns ErrUnsupportedVersion if the version is unknown.
func LoadTaskState(dir, taskID string) (*TaskState, error) {
	path := filepath.Join(dir, taskID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var state TaskState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal task state: %w", err)
	}

	if state.Version != taskStateVersion {
		return nil, fmt.Errorf(
			"%w: got %d, want %d",
			ErrUnsupportedVersion, state.Version, taskStateVersion,
		)
	}

	// Verify checksum.
	savedChecksum := state.Checksum
	state.Checksum = ""
	content, err := json.Marshal(&state)
	if err != nil {
		return nil, fmt.Errorf("marshal for verify: %w", err)
	}
	h := sha256.Sum256(content)
	computed := hex.EncodeToString(h[:])

	if computed != savedChecksum {
		return nil, fmt.Errorf(
			"%w: expected %s, got %s",
			ErrCorruptArtifact, savedChecksum, computed,
		)
	}
	state.Checksum = savedChecksum
	return &state, nil
}

// BuildAgentContext creates the context string injected into an
// agent's system prompt. Each role sees only what it needs.
func BuildAgentContext(state *TaskState, role string) string {
	switch role {
	case "lead":
		return fmt.Sprintf(
			"Task: %s\nPhase: %s\nPlan: %v\n"+
				"Progress: %v\nDecisions: %v",
			state.TaskID, state.Phase,
			state.Plan, state.Progress, state.Decisions,
		)
	case "implementer":
		return fmt.Sprintf(
			"Current phase: %s\n"+
				"Your task: implement the current step.\nPlan: %v",
			state.Phase, state.Plan,
		)
	case "reviewer":
		return fmt.Sprintf(
			"Review the changes for phase: %s\n"+
				"Plan: %v\nDecisions so far: %v",
			state.Phase, state.Plan, state.Decisions,
		)
	default:
		return fmt.Sprintf("Phase: %s", state.Phase)
	}
}
