// Package workflow provides an optional structured workflow mode
// inspired by oh-my-codex patterns. Activated via "altcode workflow".
//
// Classic "altcode" behavior is completely unaffected.
// Workflow mode adds: keyword routing, deep-interview, consensus
// planning, and persistent execution loops with state tracking.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Mode identifies a workflow mode.
type Mode string

const (
	ModeInterview Mode = "interview"
	ModePlan      Mode = "plan"
	ModeExecute   Mode = "execute"
	ModeRalph     Mode = "ralph"
)

// Phase tracks where a mode is in its lifecycle.
type Phase string

const (
	PhasePending   Phase = "pending"
	PhaseActive    Phase = "active"
	PhasePaused    Phase = "paused"
	PhaseVerifying Phase = "verifying"
	PhaseComplete  Phase = "complete"
	PhaseCancelled Phase = "cancelled"
)

// State holds the persistent state for a workflow mode.
type State struct {
	Mode       Mode      `json:"mode"`
	Phase      Phase     `json:"phase"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Iteration  int       `json:"iteration"`
	MaxIter    int       `json:"max_iterations"`
	Context    string    `json:"context,omitempty"`
	PlanPath   string    `json:"plan_path,omitempty"`
	SpecPath   string    `json:"spec_path,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// StateDir returns the workflow state directory for a project.
func StateDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".altcode", "state")
}

// SaveState writes mode state to .altcode/state/<mode>.json.
func SaveState(projectRoot string, s *State) error {
	dir := StateDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, string(s.Mode)+".json"), data, 0o644)
}

// LoadState reads mode state from disk.
func LoadState(projectRoot string, mode Mode) (*State, error) {
	path := filepath.Join(StateDir(projectRoot), string(mode)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ClearState removes mode state.
func ClearState(projectRoot string, mode Mode) error {
	path := filepath.Join(StateDir(projectRoot), string(mode)+".json")
	return os.Remove(path)
}

// ClearAll removes all workflow state JSON files.
func ClearAll(projectRoot string) error {
	dir := StateDir(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ListActive returns all modes with active (non-complete) state.
func ListActive(projectRoot string) []State {
	dir := StateDir(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var active []State
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s State
		if json.Unmarshal(data, &s) == nil && s.Phase != PhaseComplete && s.Phase != PhaseCancelled {
			active = append(active, s)
		}
	}
	return active
}

// CancelActive marks all active workflows as cancelled and returns the count.
func CancelActive(projectRoot string) int {
	active := ListActive(projectRoot)
	cancelled := 0
	for i := range active {
		active[i].Phase = PhaseCancelled
		if err := SaveState(projectRoot, &active[i]); err != nil {
			continue
		}
		cancelled++
	}
	return cancelled
}

// PauseActive marks active and verifying workflows as paused and returns the count.
func PauseActive(projectRoot string) int {
	active := ListActive(projectRoot)
	paused := 0
	for i := range active {
		if active[i].Phase != PhaseActive && active[i].Phase != PhaseVerifying {
			continue
		}
		active[i].Phase = PhasePaused
		if err := SaveState(projectRoot, &active[i]); err != nil {
			continue
		}
		paused++
	}
	return paused
}

// ResumeActive marks paused workflows as active and returns the count.
func ResumeActive(projectRoot string) int {
	active := ListActive(projectRoot)
	resumed := 0
	for i := range active {
		if active[i].Phase != PhasePaused {
			continue
		}
		active[i].Phase = PhaseActive
		if err := SaveState(projectRoot, &active[i]); err != nil {
			continue
		}
		resumed++
	}
	return resumed
}

// Summary returns a one-line summary of active workflows.
func Summary(projectRoot string) string {
	active := ListActive(projectRoot)
	if len(active) == 0 {
		return "no active workflows"
	}

	parts := make([]string, 0, len(active))
	for _, s := range active {
		parts = append(parts, fmt.Sprintf("%s: %s", s.Mode, s.Phase))
	}
	return strings.Join(parts, ", ")
}

// StatusText returns a human-readable summary of all workflow state.
func StatusText(projectRoot string) string {
	active := ListActive(projectRoot)
	if len(active) == 0 {
		return "No active workflows."
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Active workflows (%d):\n", len(active)))
	for _, s := range active {
		sb.WriteString(fmt.Sprintf("  %-12s %-10s iter %d/%d  %s\n",
			s.Mode, s.Phase, s.Iteration, s.MaxIter,
			s.UpdatedAt.Format(time.TimeOnly)))
	}
	return sb.String()
}
