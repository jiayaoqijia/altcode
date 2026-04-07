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
