package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TurnCheckpoint records a single agent turn snapshot.
type TurnCheckpoint struct {
	Turn         int       `json:"turn"`
	CommitHash   string    `json:"commit_hash"`
	Role         string    `json:"role"`
	Branch       string    `json:"branch"`
	WorktreePath string    `json:"worktree_path"`
	Summary      string    `json:"summary,omitempty"`
	FilesChanged []string  `json:"files_changed,omitempty"`
	TokensUsed   int       `json:"tokens_used,omitempty"`
	CostUSD      float64   `json:"cost_usd,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// WriteCheckpoint writes a checkpoint file to
// {dir}/turn-{NNN}-{role}.json. The role suffix prevents multi-agent
// sessions from last-writer-wins overwriting each other's metadata:
// previously lead + implementer + reviewer all writing their "first
// resting turn" produced a single turn-001.json that lost two of the
// three agents' checkpoints. Codex round-Q adversarial finding.
//
// Checkpoints contain session/worktree metadata — tighten permissions
// to 0o700 for the directory and 0o600 for individual files so other
// local users can't snoop on in-progress session state.
func WriteCheckpoint(dir string, cp TurnCheckpoint) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir checkpoints: %w", err)
	}
	name := fmt.Sprintf("turn-%03d-%s.json", cp.Turn, safeRole(cp.Role))
	p := filepath.Join(dir, name)
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	// Atomic write: tmp + rename (consistent with store.SaveSession)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write checkpoint tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		// Cleanup tmp best-effort. If removal fails, wrap both errors
		// so operators can see what actually happened on disk.
		if rmErr := os.Remove(tmp); rmErr != nil {
			return fmt.Errorf("rename checkpoint: %w (also failed to remove temp %s: %v)", err, tmp, rmErr)
		}
		return fmt.Errorf("rename checkpoint: %w", err)
	}
	return nil
}

// ReadCheckpoint reads a single checkpoint by turn number. Since
// WriteCheckpoint now namespaces by role (turn-NNN-{role}.json),
// ReadCheckpoint globs every matching role and returns the first
// hit — preserving the old single-argument API for callers that
// don't care which role produced the snapshot. For multi-agent
// sessions, callers should use ListCheckpoints + filter by Role.
// Legacy turn-NNN.json files (written before the rename) are also
// found so older sessions still load.
func ReadCheckpoint(dir string, turn int) (*TurnCheckpoint, error) {
	glob := filepath.Join(dir, fmt.Sprintf("turn-%03d*.json", turn))
	matches, err := filepath.Glob(glob)
	if err != nil {
		return nil, fmt.Errorf("glob checkpoint %d: %w", turn, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("read checkpoint %d: not found", turn)
	}
	// Prefer the legacy unsuffixed form if it exists (backward compat);
	// otherwise first match wins.
	legacy := filepath.Join(dir, fmt.Sprintf("turn-%03d.json", turn))
	for _, m := range matches {
		if m == legacy {
			matches = []string{m}
			break
		}
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, fmt.Errorf("read checkpoint %d: %w", turn, err)
	}
	var cp TurnCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint %d: %w", turn, err)
	}
	return &cp, nil
}

// ListCheckpoints returns all checkpoints in dir, sorted by turn number.
func ListCheckpoints(dir string) ([]TurnCheckpoint, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "turn-*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob checkpoints: %w", err)
	}
	if len(matches) == 0 {
		return nil, nil
	}
	var cps []TurnCheckpoint
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", m, err)
		}
		var cp TurnCheckpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			return nil, fmt.Errorf("unmarshal %s: %w", m, err)
		}
		cps = append(cps, cp)
	}
	sort.Slice(cps, func(i, j int) bool {
		return cps[i].Turn < cps[j].Turn
	})
	return cps, nil
}

// safeRole normalises a role string for use in a checkpoint filename.
// Missing roles collapse to "default" so the old schema (single-agent
// runs that didn't populate Role) still produce a valid name. Path
// separators and control chars are stripped so a crafted role can't
// escape the checkpoint directory.
func safeRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return "default"
	}
	var sb strings.Builder
	for _, r := range role {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "default"
	}
	return sb.String()
}
