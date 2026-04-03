package sandbox

import (
	"fmt"
	"strings"
)

// Policy controls command restriction level.
type Policy int

const (
	PolicyNone     Policy = iota // no restrictions
	PolicyReadOnly               // block write commands
	PolicySafe                   // block destructive commands
	PolicyStrict                 // whitelist only
)

// Sandbox evaluates commands against a security policy.
type Sandbox struct {
	policy      Policy
	blockedCmds []string
	allowedCmds []string
}

// New creates a Sandbox with the given policy and default patterns.
func New(policy Policy) *Sandbox {
	s := &Sandbox{policy: policy}
	switch policy {
	case PolicySafe:
		s.blockedCmds = defaultSafeBlockList()
	case PolicyReadOnly:
		s.blockedCmds = defaultReadOnlyBlockList()
	}
	return s
}

// Policy returns the current sandbox policy.
func (s *Sandbox) Policy() Policy { return s.policy }

// Check returns an error if the command is blocked by policy.
func (s *Sandbox) Check(command string) error {
	if s.policy == PolicyNone {
		return nil
	}
	if s.policy == PolicyStrict {
		return s.checkStrict(command)
	}
	return s.checkBlocked(command)
}

// Wrap prepends safety flags (timeout, ulimit) to a command.
func (s *Sandbox) Wrap(command string) string {
	if s.policy == PolicyNone {
		return command
	}
	return fmt.Sprintf(
		"ulimit -v 4194304 2>/dev/null; timeout 120 bash -c %s",
		shellQuote(command),
	)
}

// AddBlocked appends additional patterns to the block list.
func (s *Sandbox) AddBlocked(patterns ...string) {
	s.blockedCmds = append(s.blockedCmds, patterns...)
}

// AddAllowed appends patterns to the allow list (strict mode).
func (s *Sandbox) AddAllowed(patterns ...string) {
	s.allowedCmds = append(s.allowedCmds, patterns...)
}

func (s *Sandbox) checkBlocked(command string) error {
	normalized := strings.TrimSpace(command)
	for _, pattern := range s.blockedCmds {
		if matchPattern(normalized, pattern) {
			return fmt.Errorf(
				"command blocked by sandbox policy: %q matches %q",
				truncateCmd(command), pattern,
			)
		}
	}
	return nil
}

func (s *Sandbox) checkStrict(command string) error {
	normalized := strings.TrimSpace(command)
	for _, pattern := range s.allowedCmds {
		if matchPattern(normalized, pattern) {
			return nil
		}
	}
	return fmt.Errorf(
		"command not in allowlist: %q",
		truncateCmd(command),
	)
}

func matchPattern(cmd, pattern string) bool {
	return strings.Contains(cmd, pattern)
}

func truncateCmd(cmd string) string {
	if len(cmd) > 80 {
		return cmd[:80] + "..."
	}
	return cmd
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
