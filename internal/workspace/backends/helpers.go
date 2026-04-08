package backends

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// Activity detection thresholds (matches lifecycle constants).
const (
	ActiveWindow   int64 = 30_000  // 30s in ms
	ReadyThreshold int64 = 300_000 // 5min in ms
)

// maxTurns returns the requested turn count or the default if zero.
func maxTurns(requested, defaultMax int) int {
	if requested > 0 {
		return requested
	}
	return defaultMax
}

// checkPID parses a "pid:1234" handle and sends signal 0 to test liveness.
func checkPID(handleID string) (bool, error) {
	parts := strings.SplitN(handleID, ":", 2)
	if len(parts) != 2 || parts[0] != "pid" {
		return false, fmt.Errorf("invalid handle format: %q", handleID)
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		return false, fmt.Errorf("invalid pid in handle %q: %w", handleID, err)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, nil
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	// EPERM means the process exists but is owned by another user.
	// In multi-user/container setups, treat this as alive.
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return false, nil
}

// jsonlEntry represents one line from an activity JSONL file.
type jsonlEntry struct {
	Type      string  `json:"type"`
	State     string  `json:"state"`
	Timestamp string  `json:"timestamp"`
	SessionID string  `json:"session_id"`
	Cost      float64 `json:"cost_usd"`
	Summary   string  `json:"summary"`
	Tokens    int     `json:"tokens"`
}

// readLastJSONLEntry reads the last non-empty line from a JSONL file.
func readLastJSONLEntry(path string) (*jsonlEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var last string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			last = line
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if last == "" {
		return nil, fmt.Errorf("empty JSONL file: %s", path)
	}
	var entry jsonlEntry
	if err := json.Unmarshal([]byte(last), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

// checkActionableState returns an ActivityDetection if the JSONL entry
// represents a waiting_input or blocked state, nil otherwise.
func checkActionableState(entry *jsonlEntry) *workspace.ActivityDetection {
	switch entry.State {
	case "waiting_input":
		return &workspace.ActivityDetection{
			State:     workspace.ActivityWaitInput,
			Timestamp: parseTime(entry.Timestamp),
			Source:    "jsonl_actionable",
		}
	case "blocked":
		return &workspace.ActivityDetection{
			State:     workspace.ActivityBlocked,
			Timestamp: parseTime(entry.Timestamp),
			Source:    "jsonl_actionable",
		}
	}
	return nil
}

// jsonlFallbackState uses the age of the last JSONL entry to infer
// activity state: active (within activeWindowMs), ready (within
// thresholdMs), or idle (older).
func jsonlFallbackState(
	path string, activeWindowMs, thresholdMs int64,
) (*workspace.ActivityDetection, error) {
	entry, err := readLastJSONLEntry(path)
	if err != nil {
		// No JSONL data — agent just spawned or file is corrupt.
		// Return spawning (not active) to avoid masking stuck agents.
		return &workspace.ActivityDetection{
			State:     workspace.ActivitySpawning,
			Timestamp: time.Now(),
			Source:    "jsonl_missing",
		}, nil
	}
	ts := parseTime(entry.Timestamp)
	age := time.Since(ts).Milliseconds()

	var state workspace.ActivityState
	switch {
	case age < activeWindowMs:
		state = workspace.ActivityActive
	case age < thresholdMs:
		state = workspace.ActivityReady
	default:
		state = workspace.ActivityIdle
	}
	return &workspace.ActivityDetection{
		State:     state,
		Timestamp: ts,
		Source:    "jsonl_age",
	}, nil
}

// installPathWrappers writes git and gh wrapper scripts into
// ~/.altcode/bin/ that capture metadata before calling the real binary.
func installPathWrappers(workspacePath string) error {
	binDir := filepath.Join(os.Getenv("HOME"), ".altcode", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	for _, tool := range []string{"git", "gh"} {
		// Resolve the REAL binary, filtering out ~/.altcode/bin/ from PATH
		// to prevent self-loop on reinstall (security audit finding).
		realBin, err := lookPathExcluding(tool, binDir)
		if err != nil {
			continue // tool not installed, skip wrapper
		}

		wrapper := filepath.Join(binDir, tool)
		content := fmt.Sprintf(
			"#!/bin/sh\n# altcode PATH wrapper for %s\n"+
				"# Resolved real binary at install time to prevent self-loop.\n"+
				"exec %s \"$@\"\n",
			tool, shellEscape(realBin),
		)
		if err := os.WriteFile(wrapper, []byte(content), 0o755); err != nil {
			return fmt.Errorf("write %s wrapper: %w", tool, err)
		}
	}
	return nil
}

// writeClaudeHooks MERGES a PostToolUse hook into .claude/settings.json.
// If settings.json already exists, its existing content is preserved and
// the altcode hook is appended to the PostToolUse array. This prevents
// overwriting repo-specific permission restrictions or existing hooks.
func writeClaudeHooks(settingsPath string, sess *workspace.AgentSession) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}
	agentsDir := filepath.Join(sess.WorkspacePath, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}
	// Write a safe capture script (no shell injection possible)
	scriptPath := filepath.Join(agentsDir, "capture-metadata.sh")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$ALTCODE_PR_URL,$ALTCODE_COMMIT\" >> %s\n",
		shellEscape(filepath.Join(agentsDir, sess.Role+".log")))
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return err
	}

	// Read existing settings (if any)
	var settings map[string]any
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = map[string]any{}
	}

	// Ensure hooks.PostToolUse array exists
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}
	postHooks, _ := hooks["PostToolUse"].([]any)

	// Append our hook (idempotent — check if already present)
	altcodeHook := map[string]any{
		"matcher": "Bash",
		"hooks": []map[string]string{
			{"type": "command", "command": scriptPath},
		},
	}
	alreadyInstalled := false
	for _, h := range postHooks {
		if hm, ok := h.(map[string]any); ok {
			if hs, _ := hm["hooks"].([]any); len(hs) > 0 {
				if hh, ok := hs[0].(map[string]any); ok {
					if cmd, _ := hh["command"].(string); cmd == scriptPath {
						alreadyInstalled = true
						break
					}
				}
			}
		}
	}
	if !alreadyInstalled {
		postHooks = append(postHooks, altcodeHook)
		hooks["PostToolUse"] = postHooks
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, data, 0o644)
}

// lookPathExcluding finds a binary on PATH, skipping the given directory.
// Prevents self-loop when ~/.altcode/bin/ is already on PATH from a prior install.
func lookPathExcluding(name, excludeDir string) (string, error) {
	excludeClean := filepath.Clean(excludeDir)
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if filepath.Clean(dir) == excludeClean {
			continue
		}
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			abs, _ := filepath.Abs(candidate)
			return abs, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH (excluding %s)", name, excludeDir)
}

// shellEscape wraps a string in single quotes for safe shell use.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// parseClaudeSessionInfo reads the JSONL file and extracts session info.
func parseClaudeSessionInfo(path string) (*workspace.AgentSessionInfo, error) {
	entry, err := readLastJSONLEntry(path)
	if err != nil {
		return nil, err
	}
	return &workspace.AgentSessionInfo{
		Summary:   entry.Summary,
		Cost:      entry.Cost,
		SessionID: entry.SessionID,
		Tokens:    entry.Tokens,
	}, nil
}

// parseTime parses an RFC3339 timestamp, returning time.Now() on failure.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now()
	}
	return t
}
