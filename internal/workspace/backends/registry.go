package backends

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// NewBackend returns the Agent implementation for the given backend name.
// Supported: "claude", "codex", "opencode", "aider".
func NewBackend(name string) (workspace.Agent, error) {
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

// DetectBackends probes PATH for known coding-agent binaries and returns
// those found, in preference order (claude > codex > opencode > aider).
// Each binary is queried with --version using a 3-second timeout.
func DetectBackends(ctx context.Context) ([]DetectedBackend, error) {
	var found []DetectedBackend
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
