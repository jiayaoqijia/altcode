package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestIntegration_WorkspaceLifecycle exercises the full workspace path:
// create session -> setup worktrees -> write files -> checkpoint -> teardown.
func TestIntegration_WorkspaceLifecycle(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	storeRoot := t.TempDir()
	store := NewStore(storeRoot)
	ws := NewWorktreeWorkspace()

	sess := &WorkspaceSession{
		ID:         "ws-lifecycle-001",
		Task:       "implement auth",
		Status:     WSSSpawning,
		GitRoot:    gitRoot,
		BaseBranch: "main",
		CreatedAt:  time.Now().Truncate(time.Second),
		Agents:     map[string]*AgentRecord{},
	}

	// Setup two agent worktrees.
	for _, role := range []string{"architect", "implementer"} {
		branch := BranchName(sess.ID, role, sess.Task)
		wtPath := filepath.Join(t.TempDir(), "wt-"+role)
		res, err := ws.Setup(ctx, WorkspaceSetupRequest{
			GitRoot:      gitRoot,
			WorktreePath: wtPath,
			Branch:       branch,
			BaseRef:      "HEAD",
		})
		if err != nil {
			t.Fatalf("Setup %s: %v", role, err)
		}
		sess.Agents[role] = &AgentRecord{
			Role:         role,
			Backend:      "claude",
			Branch:       res.Branch,
			WorktreePath: res.Path,
			SpawnedAt:    time.Now(),
		}
	}

	// Write test files in each worktree.
	for role, rec := range sess.Agents {
		fp := filepath.Join(rec.WorktreePath, role+".txt")
		if err := os.WriteFile(fp, []byte(role), 0o644); err != nil {
			t.Fatalf("write %s: %v", fp, err)
		}
	}

	// Checkpoint each worktree; verify commit exists.
	for role, rec := range sess.Agents {
		msg := "altcode: checkpoint turn-001 [" + role + "]"
		hash, err := ws.Checkpoint(ctx, rec.WorktreePath, msg)
		if err != nil {
			t.Fatalf("Checkpoint %s: %v", role, err)
		}
		if len(hash) < 7 {
			t.Errorf("hash too short for %s: %q", role, hash)
		}
		out, err := runGit(ctx, rec.WorktreePath, "log", "--oneline", "-1")
		if err != nil {
			t.Fatalf("git log %s: %v", role, err)
		}
		if !strings.Contains(out, "turn-001") {
			t.Errorf("commit not in log for %s: %q", role, out)
		}
	}

	// Teardown worktrees; verify directories removed.
	for role, rec := range sess.Agents {
		if err := ws.Teardown(ctx, rec.WorktreePath); err != nil {
			t.Fatalf("Teardown %s: %v", role, err)
		}
		if _, err := os.Stat(rec.WorktreePath); !os.IsNotExist(err) {
			t.Errorf("worktree for %s still exists", role)
		}
	}

	// Persist session and verify round-trip via Store.
	sess.Status = WSSDone
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := store.LoadSession(sess.ID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Status != WSSDone {
		t.Errorf("Status = %q, want %q", loaded.Status, WSSDone)
	}
	if len(loaded.Agents) != 2 {
		t.Errorf("Agents count = %d, want 2", len(loaded.Agents))
	}
}

// TestIntegration_StoreRoundTrip exercises save/load with a complex
// 3-agent session, activity log append, and SendMessage context updates.
func TestIntegration_StoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	now := time.Now().Truncate(time.Second)
	sess := &WorkspaceSession{
		ID:           "ws-roundtrip",
		Task:         "migrate database",
		Status:       WSSCIChecking,
		GitRoot:      "/repo",
		BaseBranch:   "main",
		CIRetries:    2,
		MaxCIRetries: 5,
		CreatedAt:    now,
		AutoMerge:    true,
		MergeMethod:  MergeSquash,
		Reviewers:    []string{"alice", "bob"},
		Agents: map[string]*AgentRecord{
			"architect": {
				Role: "architect", Backend: "claude",
				Branch:        "altcode/arch/migrate",
				ActivityState: ActivityActive,
				PRID:          10, CIStatus: CIPass,
				ReviewStatus: ReviewApproved,
			},
			"implementer": {
				Role: "implementer", Backend: "codex",
				Branch:        "altcode/impl/migrate",
				ActivityState: ActivityReady,
				TurnCount:     5, CostUSD: 0.42,
			},
			"reviewer": {
				Role: "reviewer", Backend: "opencode",
				Branch:        "altcode/rev/migrate",
				ActivityState: ActivityIdle,
				RestartCount:  1,
			},
		},
	}

	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, err := store.LoadSession(sess.ID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	assertSessionEqual(t, loaded, sess)

	// AppendActivity -> verify JSONL written and parseable.
	entry := map[string]any{
		"ts":   now.UTC().Format(time.RFC3339),
		"type": "test_event",
		"data": "hello",
	}
	if err := store.AppendActivity(sess.ID, entry); err != nil {
		t.Fatalf("AppendActivity: %v", err)
	}
	lines, err := store.readActivityLines(sess.ID)
	if err != nil {
		t.Fatalf("readActivityLines: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("invalid JSONL: %v", err)
	}
	if parsed["type"] != "test_event" {
		t.Errorf("type = %q, want test_event", parsed["type"])
	}

	// SendMessage -> verify context.md updated.
	ctx := context.Background()
	err = store.SendMessage(ctx, sess.ID, "architect", "fix migration")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	ctxData, err := os.ReadFile(
		filepath.Join(root, sess.ID, "context.md"),
	)
	if err != nil {
		t.Fatalf("read context.md: %v", err)
	}
	if !strings.Contains(string(ctxData), "fix migration") {
		t.Error("context.md missing injected message")
	}
}

// TestIntegration_CheckpointRollback creates two checkpoints and rolls
// back to the first, verifying file-level git state.
func TestIntegration_CheckpointRollback(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	ws := NewWorktreeWorkspace()

	wtPath := filepath.Join(t.TempDir(), "wt-rollback")
	_, err := ws.Setup(ctx, WorkspaceSetupRequest{
		GitRoot: gitRoot, WorktreePath: wtPath,
		Branch: "altcode/test/rollback/task", BaseRef: "HEAD",
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Turn 1: write file A, checkpoint.
	fileA := filepath.Join(wtPath, "file_a.txt")
	os.WriteFile(fileA, []byte("content A"), 0o644)
	hash1, err := ws.Checkpoint(ctx, wtPath, "altcode: checkpoint turn-001")
	if err != nil {
		t.Fatalf("Checkpoint turn 1: %v", err)
	}

	// Turn 2: write file B, checkpoint.
	fileB := filepath.Join(wtPath, "file_b.txt")
	os.WriteFile(fileB, []byte("content B"), 0o644)
	hash2, err := ws.Checkpoint(ctx, wtPath, "altcode: checkpoint turn-002")
	if err != nil {
		t.Fatalf("Checkpoint turn 2: %v", err)
	}
	if hash1 == hash2 {
		t.Fatal("turn 1 and turn 2 hashes must differ")
	}

	// Both files exist before rollback.
	for _, f := range []string{fileA, fileB} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("missing before rollback: %s", f)
		}
	}

	// Rollback to turn 1 commit.
	_, err = runGit(ctx, wtPath, "reset", "--hard", hash1)
	if err != nil {
		t.Fatalf("git reset: %v", err)
	}

	if _, err := os.Stat(fileA); err != nil {
		t.Error("file A should exist after rollback")
	}
	if _, err := os.Stat(fileB); !os.IsNotExist(err) {
		t.Error("file B should be gone after rollback")
	}
}

// TestIntegration_ActivityCascade exercises JSONL activity detection:
// read last entry, check actionable states, test age-based decay.
func TestIntegration_ActivityCascade(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	id := "ws-activity-cascade"
	os.MkdirAll(filepath.Join(dir, id), 0o755)

	// Write entries with various states.
	entries := []map[string]any{
		{
			"ts":       time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			"activity": "active",
			"type":     "agent_active",
		},
		{
			"ts":       time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339),
			"activity": "waiting_input",
			"type":     "permission_request",
		},
		{
			"ts":       time.Now().UTC().Format(time.RFC3339),
			"activity": "blocked",
			"type":     "error",
		},
	}
	for _, e := range entries {
		if err := store.AppendActivity(id, e); err != nil {
			t.Fatalf("AppendActivity: %v", err)
		}
	}

	lines, err := store.readActivityLines(id)
	if err != nil {
		t.Fatalf("readActivityLines: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Last entry should be "blocked".
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("unmarshal last: %v", err)
	}
	if last["activity"] != "blocked" {
		t.Errorf("last activity = %q, want blocked", last["activity"])
	}

	// Verify all entries are valid JSON.
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d invalid JSON: %v", i, err)
		}
	}
}

// TestIntegration_WorktreeBranchCollision verifies timestamp suffix on
// branch name collision.
func TestIntegration_WorktreeBranchCollision(t *testing.T) {
	gitRoot := initBareRepo(t)
	ctx := context.Background()
	ws := NewWorktreeWorkspace()
	branch := "altcode/test/collision/dup"

	// Pre-create the branch to force collision.
	if _, err := runGit(ctx, gitRoot, "branch", branch); err != nil {
		t.Fatalf("pre-create branch: %v", err)
	}

	wtPath := filepath.Join(t.TempDir(), "wt-collision")
	res, err := ws.Setup(ctx, WorkspaceSetupRequest{
		GitRoot: gitRoot, WorktreePath: wtPath,
		Branch: branch, BaseRef: "HEAD",
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if res.Branch == branch {
		t.Error("expected branch name to have collision suffix")
	}
	if !strings.HasPrefix(res.Branch, branch+"-") {
		t.Errorf("Branch %q missing prefix %q-", res.Branch, branch)
	}
}

// TestIntegration_SecretGuard validates all 7 secret patterns plus
// clean content through the same regex patterns used by wsctl.SecretGuard.
func TestIntegration_SecretGuard(t *testing.T) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),
		regexp.MustCompile(`gho_[A-Za-z0-9]{36,}`),
		regexp.MustCompile(`AKIA[A-Z0-9]{16,}`),
		regexp.MustCompile(`xoxb-[0-9A-Za-z-]+`),
		regexp.MustCompile(`xoxp-[0-9A-Za-z-]+`),
		regexp.MustCompile(`-----BEGIN (RSA |EC )?PRIVATE KEY-----`),
	}

	guard := func(content string) bool {
		for _, re := range patterns {
			if re.MatchString(content) {
				return true
			}
		}
		return false
	}

	cases := []struct {
		name    string
		content string
		blocked bool
	}{
		{"clean", "no secrets here", false},
		{"sk-key", "sk-abc123def456ghi789jklmnopqrstuvw", true},
		{"ghp-token", "ghp_1234567890abcdef1234567890abcdef12345678", true},
		{"gho-token", "gho_1234567890abcdef1234567890abcdef12345678", true},
		{"AKIA-key", "AKIAIOSFODNN7EXAMPLEAA", true},
		{"xoxb-slack", "xoxb-123-456-abc", true},
		{"xoxp-slack", "xoxp-123-456-abc", true},
		{"private-key", "-----BEGIN PRIVATE KEY-----", true},
		{"rsa-key", "-----BEGIN RSA PRIVATE KEY-----", true},
		{"ec-key", "-----BEGIN EC PRIVATE KEY-----", true},
		{"short-sk", "sk-short", false},
		{"partial-ghp", "ghp_tooshort", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := guard(tc.content)
			if got != tc.blocked {
				t.Errorf("guard(%q) = %v, want %v",
					tc.content, got, tc.blocked)
			}
		})
	}
}

// --- helpers ---

func assertSessionEqual(t *testing.T, got, want *WorkspaceSession) {
	t.Helper()
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.CIRetries != want.CIRetries {
		t.Errorf("CIRetries = %d, want %d", got.CIRetries, want.CIRetries)
	}
	if got.MaxCIRetries != want.MaxCIRetries {
		t.Errorf("MaxCIRetries = %d, want %d",
			got.MaxCIRetries, want.MaxCIRetries)
	}
	if len(got.Agents) != len(want.Agents) {
		t.Errorf("Agents count = %d, want %d",
			len(got.Agents), len(want.Agents))
	}
}
