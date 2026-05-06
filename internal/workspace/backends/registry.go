package backends

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

// NewBackend returns the Agent implementation for the given backend name.
// It first checks YAML-defined agents from agentDefs, then falls back
// to hardcoded backends: "claude", "codex", "opencode", "aider".
func NewBackend(name string) (workspace.Agent, error) {
	if def, ok := agentDefs[name]; ok {
		return NewUniversalBackend(def), nil
	}
	switch name {
	case "claude":
		return &ClaudeBackend{}, nil
	case "codex":
		return &CodexBackend{}, nil
	case "opencode":
		return &OpenCodeBackend{}, nil
	case "aider":
		return &AiderBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown backend: %q", name)
	}
}

// agentDefs holds discovered YAML agent definitions, keyed by name.
var agentDefs = map[string]*AgentDef{}

// DetectedBackend describes a coding-agent binary found on PATH.
type DetectedBackend struct {
	Name    string // human-readable: "claude", "codex", "opencode", "aider"
	Binary  string // absolute path to the binary
	Version string // output of --version, trimmed
}

// probeSpec defines a backend to search for.
type probeSpec struct {
	name   string
	binary string
}

// probeOrder is the preference order for backend detection.
var probeOrder = []probeSpec{
	{"claude", "claude"},
	{"codex", "codex"},
	{"opencode", "opencode"},
	{"aider", "aider"},
}

// LoadAgentDefsIntoRegistry discovers YAML agent definitions from the
// given directories and registers them so NewBackend can find them.
// Hardcoded backends always take precedence over YAML definitions
// with the same name.
func LoadAgentDefsIntoRegistry(dirs ...string) error {
	defs, err := DiscoverAgentDefs(dirs...)
	if err != nil {
		return err
	}
	for _, def := range defs {
		if isHardcodedBackend(def.Name) {
			continue
		}
		agentDefs[def.Name] = def
	}
	return nil
}

// isHardcodedBackend returns true if a name is served by a native backend.
func isHardcodedBackend(name string) bool {
	switch name {
	case "claude", "codex", "opencode", "aider":
		return true
	}
	return false
}

// DetectBackends probes PATH for known coding-agent binaries and returns
// those found, in preference order (claude > codex > opencode > aider).
// It also includes YAML-defined agents whose detect command succeeds.
// Each binary is queried with --version using a 3-second timeout.
func DetectBackends(ctx context.Context) ([]DetectedBackend, error) {
	var found []DetectedBackend

	// 1. Hardcoded backends in preference order.
	for _, spec := range probeOrder {
		bin, err := exec.LookPath(spec.binary)
		if err != nil {
			continue
		}
		ver := probeVersion(ctx, bin)
		found = append(found, DetectedBackend{
			Name:    spec.name,
			Binary:  bin,
			Version: ver,
		})
	}

	// 2. YAML-defined agents (skip names already found above).
	seen := make(map[string]bool, len(found))
	for _, b := range found {
		seen[b.Name] = true
	}
	for _, def := range agentDefs {
		if seen[def.Name] {
			continue
		}
		bin, err := exec.LookPath(def.Binary)
		if err != nil {
			continue
		}
		ver := probeVersion(ctx, bin)
		found = append(found, DetectedBackend{
			Name:    def.Name,
			Binary:  bin,
			Version: ver,
		})
	}

	return found, nil
}

// probeVersion runs "{binary} --version" with a 3s timeout and returns
// the first line of stdout, trimmed. Returns "" on any error.
func probeVersion(ctx context.Context, binary string) string {
	vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(vctx, binary, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return ""
	}
	s := strings.TrimSpace(out.String())
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return s
}
