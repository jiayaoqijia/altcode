package checkpoint

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadCheckpoint(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "checkpoints")

	now := time.Now().Truncate(time.Second)
	cp := TurnCheckpoint{
		Turn:       1,
		CommitHash: "abc123def456",
		Role:       "architect",
		CreatedAt:  now,
	}
	if err := WriteCheckpoint(dir, cp); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	loaded, err := ReadCheckpoint(dir, 1)
	if err != nil {
		t.Fatalf("ReadCheckpoint: %v", err)
	}
	if loaded.Turn != cp.Turn {
		t.Errorf("Turn = %d, want %d", loaded.Turn, cp.Turn)
	}
	if loaded.CommitHash != cp.CommitHash {
		t.Errorf("CommitHash = %q, want %q", loaded.CommitHash, cp.CommitHash)
	}
	if loaded.Role != cp.Role {
		t.Errorf("Role = %q, want %q", loaded.Role, cp.Role)
	}
	if !loaded.CreatedAt.Equal(cp.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", loaded.CreatedAt, cp.CreatedAt)
	}
}

func TestListCheckpoints(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "checkpoints")

	// Empty directory returns nil.
	cps, err := ListCheckpoints(dir)
	if err != nil {
		t.Fatalf("ListCheckpoints empty: %v", err)
	}
	if len(cps) != 0 {
		t.Fatalf("expected 0 checkpoints, got %d", len(cps))
	}

	// Write several checkpoints out of order.
	now := time.Now().Truncate(time.Second)
	for _, turn := range []int{3, 1, 2} {
		cp := TurnCheckpoint{
			Turn:       turn,
			CommitHash: "hash-" + string(rune('0'+turn)),
			Role:       "impl",
			CreatedAt:  now.Add(time.Duration(turn) * time.Minute),
		}
		if err := WriteCheckpoint(dir, cp); err != nil {
			t.Fatalf("WriteCheckpoint(%d): %v", turn, err)
		}
	}

	cps, err = ListCheckpoints(dir)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 3 {
		t.Fatalf("expected 3 checkpoints, got %d", len(cps))
	}
	// Must be sorted by turn.
	for i, cp := range cps {
		if cp.Turn != i+1 {
			t.Errorf("cps[%d].Turn = %d, want %d", i, cp.Turn, i+1)
		}
	}
}

func TestReadCheckpoint_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadCheckpoint(dir, 99)
	if err == nil {
		t.Fatal("expected error for missing checkpoint")
	}
}
