package backends

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

// AgentDef is the YAML schema for a pluggable agent definition.
type AgentDef struct {
	Name       string   `yaml:"name"`
	Binary     string   `yaml:"binary"`
	Args       []string `yaml:"args"`
	TaskFlag   string   `yaml:"task_flag"`
	Mode       string   `yaml:"mode"`
	Worktree   bool     `yaml:"worktree"`
	TUI        bool     `yaml:"tui"` // launch in tmux pane with real PTY
	ResumeFlag string   `yaml:"resume_flag"`
	DetectCmd  string   `yaml:"detect"`
}

// UniversalBackend implements workspace.Agent for any CLI agent
// described by an AgentDef YAML file.
type UniversalBackend struct {
	def AgentDef
}

// NewUniversalBackend creates a backend from a parsed agent definition.
func NewUniversalBackend(def *AgentDef) *UniversalBackend {
	return &UniversalBackend{def: *def}
}

func (b *UniversalBackend) Name() string { return b.def.Name }

func (b *UniversalBackend) LaunchCommand(
	sess *workspace.AgentSession,
) ([]string, error) {
	argv := []string{b.def.Binary}
	argv = append(argv, b.def.Args...)
	if b.def.TaskFlag != "" {
		argv = append(argv, b.def.TaskFlag, sess.Task)
	} else {
		argv = append(argv, sess.Task)
	}
	return argv, nil
}

func (b *UniversalBackend) RestoreCommand(
	sess *workspace.AgentSession,
) ([]string, error) {
	if b.def.ResumeFlag == "" || sess.PriorSessionID == "" {
		return nil, nil
	}
	argv := []string{b.def.Binary}
	argv = append(argv, b.def.Args...)
	argv = append(argv, b.def.ResumeFlag, sess.PriorSessionID)
	if b.def.TaskFlag != "" {
		argv = append(argv, b.def.TaskFlag, sess.Task)
	} else {
		argv = append(argv, sess.Task)
	}
	return argv, nil
}

func (b *UniversalBackend) Environment(
	sess *workspace.AgentSession,
) (map[string]string, error) {
	return map[string]string{
		"ALTCODE_SESSION_ID": sess.AOSessionID,
		"ALTCODE_ROLE":       sess.Role,
	}, nil
}

func (b *UniversalBackend) SetupWorkspaceHooks(
	_ *workspace.AgentSession,
) error {
	return nil
}

func (b *UniversalBackend) ActivityState(
	_ context.Context, sess *workspace.AgentSession,
) (*workspace.ActivityDetection, error) {
	alive, err := b.IsProcessRunning(sess)
	if err != nil || !alive {
		return &workspace.ActivityDetection{
			State:     workspace.ActivityExited,
			Timestamp: time.Now(),
			Source:    "process_dead",
		}, nil
	}
	return &workspace.ActivityDetection{
		State:     workspace.ActivityActive,
		Timestamp: time.Now(),
		Source:    "process_alive",
	}, nil
}

func (b *UniversalBackend) IsProcessRunning(
	sess *workspace.AgentSession,
) (bool, error) {
	if sess.RuntimeHandleID == "" {
		return false, nil
	}
	return checkPID(sess.RuntimeHandleID)
}

func (b *UniversalBackend) SessionInfo(
	_ context.Context, _ *workspace.AgentSession,
) (*workspace.AgentSessionInfo, error) {
	return nil, nil
}

// LoadAgentDef reads and parses a single agent YAML definition.
func LoadAgentDef(path string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def AgentDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	return &def, nil
}

// DiscoverAgentDefs scans directories for *.yaml agent definitions
// and returns all successfully parsed definitions. Directories that
// do not exist are silently skipped.
func DiscoverAgentDefs(dirs ...string) ([]*AgentDef, error) {
	var defs []*AgentDef
	seen := make(map[string]bool)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // directory missing is not an error
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := filepath.Ext(e.Name())
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			def, err := LoadAgentDef(filepath.Join(dir, e.Name()))
			if err != nil || def.Name == "" {
				continue
			}
			if seen[def.Name] {
				continue // first directory wins
			}
			seen[def.Name] = true
			defs = append(defs, def)
		}
	}
	return defs, nil
}
