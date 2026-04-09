package wsctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/altcode-ai/altcode/internal/workspace"
)

func TestInjectWorkspaceContext_Claude(t *testing.T) {
	dir := t.TempDir()
	sess := &workspace.WorkspaceSession{
		ID:     "01HVTEST",
		Task:   "add auth",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"architect": {
				Role:          "architect",
				Backend:       "claude",
				Branch:        "altcode/01hv/architect/add-auth",
				ActivityState: workspace.ActivityActive,
			},
		},
	}

	err := InjectWorkspaceContext(dir, "claude", "architect", "add auth", sess)
	if err != nil {
		t.Fatalf("InjectWorkspaceContext: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"**Workspace:** 01HVTEST",
		"**Your Role:** architect",
		"add auth",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}
}

func TestInjectWorkspaceContext_Codex(t *testing.T) {
	dir := t.TempDir()
	sess := &workspace.WorkspaceSession{
		ID:     "01HVTEST",
		Task:   "fix bug",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{},
	}

	err := InjectWorkspaceContext(dir, "codex", "implementer", "fix bug", sess)
	if err != nil {
		t.Fatalf("InjectWorkspaceContext: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md not created: %v", err)
	}
}

func TestInjectWorkspaceContext_MultiAgent(t *testing.T) {
	sess := &workspace.WorkspaceSession{
		ID:     "01MULTI",
		Task:   "build JWT auth",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{
			"architect": {
				Role:          "architect",
				Backend:       "claude",
				Branch:        "altcode/01MULTI/architect/build-jwt-auth",
				ActivityState: workspace.ActivityActive,
				PRID:          42,
				CIStatus:      workspace.CIPass,
			},
			"implementer": {
				Role:          "implementer",
				Backend:       "codex",
				Branch:        "altcode/01MULTI/implementer/build-jwt-auth",
				ActivityState: workspace.ActivityActive,
			},
			"reviewer": {
				Role:          "reviewer",
				Backend:       "claude",
				Branch:        "altcode/01MULTI/reviewer/build-jwt-auth",
				ActivityState: workspace.ActivityExited,
			},
			"security": {
				Role:          "security",
				Backend:       "codex",
				Branch:        "altcode/01MULTI/security/build-jwt-auth",
				ActivityState: workspace.ActivityBlocked,
			},
		},
	}

	// Each agent gets its own worktree dir with context injected
	for role, rec := range sess.Agents {
		dir := t.TempDir()
		err := InjectWorkspaceContext(dir, rec.Backend, role, "build JWT auth", sess)
		if err != nil {
			t.Fatalf("InjectWorkspaceContext for %s: %v", role, err)
		}

		// Claude agents get CLAUDE.md, codex agents get AGENTS.md
		expectedFile := "AGENTS.md"
		if rec.Backend == "claude" {
			expectedFile = "CLAUDE.md"
		}
		data, err := os.ReadFile(filepath.Join(dir, expectedFile))
		if err != nil {
			t.Fatalf("%s: expected %s to exist: %v", role, expectedFile, err)
		}
		content := string(data)

		// Each agent's context should contain its own role
		if !strings.Contains(content, "**Your Role:** "+role) {
			t.Errorf("%s: missing own role in context", role)
		}

		// Agent table should list ALL agents (cross-visibility)
		for otherRole := range sess.Agents {
			if !strings.Contains(content, otherRole) {
				t.Errorf("%s: missing other agent %q in context table", role, otherRole)
			}
		}

		// Branch names visible in table
		if !strings.Contains(content, rec.Branch) {
			t.Errorf("%s: own branch not in agent table", role)
		}
	}
}

func TestInjectWorkspaceContext_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	existing := "# My Project\n\nExisting instructions here.\n"
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(existing), 0o644)

	sess := &workspace.WorkspaceSession{
		ID:     "01PRES",
		Task:   "test preservation",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{},
	}

	err := InjectWorkspaceContext(dir, "claude", "worker", "test", sess)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	content := string(data)

	// Original content must still be there
	if !strings.Contains(content, "Existing instructions here") {
		t.Error("existing content was overwritten instead of appended")
	}
	// New workspace context also present
	if !strings.Contains(content, "Workspace Context") {
		t.Error("workspace context not injected")
	}
}

func TestSecretGuard_BlocksInjection(t *testing.T) {
	dir := t.TempDir()
	// Pre-populate with a secret in existing file
	secret := "export ANTHROPIC_API_KEY=sk-ant-abc123def456ghi789jkl012mno345pqr678stu"
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(secret), 0o644)

	sess := &workspace.WorkspaceSession{
		ID:     "01SEC",
		Task:   "test",
		Status: workspace.WSSWorking,
		Agents: map[string]*workspace.AgentRecord{},
	}

	err := InjectWorkspaceContext(dir, "claude", "worker", "test", sess)
	if err == nil {
		t.Error("expected secret guard to block injection when existing file has secrets")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Errorf("expected secret guard error, got: %v", err)
	}
}

func TestSecretGuard(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"clean", "This is safe content", false},
		{"sk-key", "Use sk-abc123def456ghi789jkl012mno", true},
		{"ghp-token", "Token: ghp_1234567890abcdef1234567890abcdef1234", true},
		{"AKIA", "AWS key AKIAIOSFODNN7EXAMPLE found", true},
		{"private-key", "-----BEGIN RSA PRIVATE KEY-----", true},
		{"no-match", "sk-short is fine", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SecretGuard(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("SecretGuard(%q) err=%v, wantErr=%v",
					tt.content, err, tt.wantErr)
			}
		})
	}
}
