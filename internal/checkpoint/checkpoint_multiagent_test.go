package checkpoint

import (
	"path/filepath"
	"testing"
	"time"
)

// TestWriteCheckpoint_MultiAgentNoCollision guards the Codex round-Q
// finding: turn-%03d.json was keyed only by turn number, so in a
// multi-agent session lead + implementer + reviewer all writing their
// first-resting-turn checkpoint produced a single turn-001.json that
// last-writer-wins overwrote two of the three. The role suffix now
// namespaces per-agent so all three persist.
func TestWriteCheckpoint_MultiAgentNoCollision(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "checkpoints")
	now := time.Now().Truncate(time.Second)

	roles := []string{"lead", "implementer", "reviewer"}
	for _, role := range roles {
		cp := TurnCheckpoint{
			Turn:       1,
			Role:       role,
			CommitHash: "sha-" + role,
			CreatedAt:  now,
		}
		if err := WriteCheckpoint(dir, cp); err != nil {
			t.Fatalf("WriteCheckpoint(%s): %v", role, err)
		}
	}

	got, err := ListCheckpoints(dir)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(got) != len(roles) {
		t.Fatalf("got %d checkpoints, want %d (each agent's turn 1 should persist separately)",
			len(got), len(roles))
	}

	// Every role's distinct commit_hash should be recovered.
	seen := make(map[string]bool)
	for _, cp := range got {
		seen[cp.CommitHash] = true
	}
	for _, role := range roles {
		if !seen["sha-"+role] {
			t.Errorf("commit for role %q not recovered; got: %+v",
				role, got)
		}
	}
}

// TestSafeRole_SanitizesPathTraversal ensures a role string like
// "../../../etc" is stripped so an agent can't escape the checkpoint
// directory through the filename.
func TestSafeRole_SanitizesPathTraversal(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"lead", "lead"},
		{"impl-1", "impl-1"},
		{"", "default"},
		{"../../etc", "etc"},
		{"role/with/slash", "rolewithslash"},
		{"role with space", "rolewithspace"},
		{"role\nwithnewline", "rolewithnewline"},
		{"just-symbols!@#$", "just-symbols"},
		{"!!!!", "default"},
	}
	for _, tc := range cases {
		if got := safeRole(tc.in); got != tc.want {
			t.Errorf("safeRole(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
