package backends

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

func TestLoadAgentDef(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: test-agent
binary: my-agent
args: ["--headless", "--json"]
task_flag: "--task"
mode: exec
worktree: true
resume_flag: "--resume"
detect: "my-agent --version"
`
	path := filepath.Join(dir, "test-agent.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := LoadAgentDef(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.Name != "test-agent" {
		t.Errorf("Name = %q, want %q", def.Name, "test-agent")
	}
	if def.Binary != "my-agent" {
		t.Errorf("Binary = %q, want %q", def.Binary, "my-agent")
	}
	if len(def.Args) != 2 || def.Args[0] != "--headless" {
		t.Errorf("Args = %v, want [--headless --json]", def.Args)
	}
	if def.TaskFlag != "--task" {
		t.Errorf("TaskFlag = %q, want %q", def.TaskFlag, "--task")
	}
	if def.Mode != "exec" {
		t.Errorf("Mode = %q, want %q", def.Mode, "exec")
	}
	if !def.Worktree {
		t.Error("Worktree = false, want true")
	}
	if def.ResumeFlag != "--resume" {
		t.Errorf("ResumeFlag = %q, want %q", def.ResumeFlag, "--resume")
	}
	if def.DetectCmd != "my-agent --version" {
		t.Errorf("DetectCmd = %q, want %q", def.DetectCmd, "my-agent --version")
	}
}

func TestLoadAgentDef_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAgentDef(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadAgentDef_MissingFile(t *testing.T) {
	_, err := LoadAgentDef("/nonexistent/path.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestUniversalBackend_LaunchCommand(t *testing.T) {
	t.Run("with task flag", func(t *testing.T) {
		def := &AgentDef{
			Name:     "claude-code",
			Binary:   "claude",
			Args:     []string{"--verbose", "--max-turns", "50"},
			TaskFlag: "-p",
		}
		b := NewUniversalBackend(def)
		sess := &workspace.AgentSession{Task: "build auth"}
		cmd, err := b.LaunchCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd, " ")
		if !strings.Contains(joined, "claude --verbose --max-turns 50 -p build auth") {
			t.Errorf("unexpected argv: %s", joined)
		}
	})

	t.Run("positional task", func(t *testing.T) {
		def := &AgentDef{
			Name:   "codex-exec",
			Binary: "codex",
			Args:   []string{"exec"},
		}
		b := NewUniversalBackend(def)
		sess := &workspace.AgentSession{Task: "fix tests"}
		cmd, err := b.LaunchCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd, " ")
		if !strings.Contains(joined, "codex exec fix tests") {
			t.Errorf("unexpected argv: %s", joined)
		}
	})
}

func TestUniversalBackend_RestoreCommand(t *testing.T) {
	t.Run("no resume flag", func(t *testing.T) {
		def := &AgentDef{Name: "test", Binary: "test"}
		b := NewUniversalBackend(def)
		sess := &workspace.AgentSession{
			Task:           "x",
			PriorSessionID: "abc",
		}
		cmd, err := b.RestoreCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		if cmd != nil {
			t.Errorf("expected nil, got %v", cmd)
		}
	})

	t.Run("no prior session", func(t *testing.T) {
		def := &AgentDef{
			Name:       "test",
			Binary:     "test",
			ResumeFlag: "--resume",
		}
		b := NewUniversalBackend(def)
		sess := &workspace.AgentSession{Task: "x"}
		cmd, err := b.RestoreCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		if cmd != nil {
			t.Errorf("expected nil, got %v", cmd)
		}
	})

	t.Run("with resume", func(t *testing.T) {
		def := &AgentDef{
			Name:       "claude-code",
			Binary:     "claude",
			Args:       []string{"--verbose"},
			TaskFlag:   "-p",
			ResumeFlag: "--resume",
		}
		b := NewUniversalBackend(def)
		sess := &workspace.AgentSession{
			Task:           "build auth",
			PriorSessionID: "sess-123",
		}
		cmd, err := b.RestoreCommand(sess)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(cmd, " ")
		if !strings.Contains(joined, "--resume sess-123") {
			t.Errorf("missing resume flag in: %s", joined)
		}
		if !strings.Contains(joined, "-p build auth") {
			t.Errorf("missing task in: %s", joined)
		}
	})
}

func TestUniversalBackend_Name(t *testing.T) {
	def := &AgentDef{Name: "my-agent", Binary: "my-agent"}
	b := NewUniversalBackend(def)
	if b.Name() != "my-agent" {
		t.Errorf("Name() = %q, want %q", b.Name(), "my-agent")
	}
}

func TestUniversalBackend_Environment(t *testing.T) {
	def := &AgentDef{Name: "test", Binary: "test"}
	b := NewUniversalBackend(def)
	sess := &workspace.AgentSession{
		AOSessionID: "ws-456",
		Role:        "architect",
	}
	env, err := b.Environment(sess)
	if err != nil {
		t.Fatal(err)
	}
	if env["ALTCODE_SESSION_ID"] != "ws-456" {
		t.Errorf("ALTCODE_SESSION_ID = %q", env["ALTCODE_SESSION_ID"])
	}
	if env["ALTCODE_ROLE"] != "architect" {
		t.Errorf("ALTCODE_ROLE = %q", env["ALTCODE_ROLE"])
	}
}

func TestDiscoverAgentDefs(t *testing.T) {
	dir := t.TempDir()

	// Write two valid YAML files and one non-YAML file.
	for name, content := range map[string]string{
		"alpha.yaml": "name: alpha\nbinary: alpha-bin\n",
		"beta.yml":   "name: beta\nbinary: beta-bin\n",
		"readme.txt": "not a yaml file",
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	defs, err := DiscoverAgentDefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta, got %v", names)
	}
}

func TestDiscoverAgentDefs_MissingDir(t *testing.T) {
	defs, err := DiscoverAgentDefs("/nonexistent/dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Errorf("expected 0 defs for missing dir, got %d", len(defs))
	}
}

func TestDiscoverAgentDefs_Dedup(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Same name in both dirs; first dir wins.
	y1 := "name: dup\nbinary: bin-v1\n"
	y2 := "name: dup\nbinary: bin-v2\n"
	os.WriteFile(filepath.Join(dir1, "dup.yaml"), []byte(y1), 0o644)
	os.WriteFile(filepath.Join(dir2, "dup.yaml"), []byte(y2), 0o644)

	defs, err := DiscoverAgentDefs(dir1, dir2)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("got %d defs, want 1", len(defs))
	}
	if defs[0].Binary != "bin-v1" {
		t.Errorf("Binary = %q, want bin-v1 (first dir wins)", defs[0].Binary)
	}
}
