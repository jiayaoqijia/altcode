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
	// Any policy stricter than "none" must block commands that embed
	// shell operators capable of subverting token-based allow/block
	// matching — redirections, process substitutions, command
	// substitutions, or piped shell invocations. `echo owned > file`
	// would otherwise pass an allow-list containing just "echo",
	// because matchPattern never sees what the redirection writes to.
	// Found by Codex round-F adversarial review.
	if reason := unsafeShellSyntax(command); reason != "" {
		return fmt.Errorf(
			"command blocked by sandbox policy: %s: %q",
			reason, truncateCmd(command),
		)
	}
	if s.policy == PolicyStrict {
		return s.checkStrict(command)
	}
	return s.checkBlocked(command)
}

// unsafeShellSyntax returns a non-empty reason if the command contains
// shell syntax that cannot be safely analysed by token-based
// allow/block matching. Quoted occurrences are part of an argument to
// one command and are ignored. Covered: redirections, process
// substitution, command substitution, AND top-level command chaining
// (`;`, `&&`, `||`, `|`, `&`, newline). Codex round-G caught that
// chaining defeated even a strict allowlist: `echo hi; touch /pwn`
// passed because "echo" appeared as a token — but the second command
// in the chain was never vetted.
func unsafeShellSyntax(command string) string {
	var inSingle, inDouble bool
	for i := 0; i < len(command); i++ {
		c := command[i]
		// Honor backslash escapes outside single quotes: bash treats
		// `\"` as a LITERAL `"` (not as opening a quoted region), and
		// `\;` / `\|` / `\>` pass the operator through as argument
		// text. Without this, an attacker could hide a chained
		// separator behind a pair of escaped quotes:
		//   echo \" ; touch /pwn
		// Before this fix we toggled inDouble on `\"` and then read
		// the `;` as "still inside quotes" — letting the chain pass.
		// Found by Codex round-I adversarial review.
		if c == '\\' && !inSingle {
			// Skip the escaped char — it's not a syntax-level
			// character regardless of what it is.
			i++
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		switch c {
		case '>':
			return "output redirection is not allowed"
		case '<':
			return "input redirection is not allowed"
		case '`':
			return "backtick command substitution is not allowed"
		case ';', '\n':
			return "command chaining is not allowed"
		case '|':
			// | (pipe) and || (or-list) both chain; treat the same.
			return "pipes and or-lists are not allowed"
		case '&':
			// & (background) and && (and-list) both chain.
			return "background and and-lists are not allowed"
		case '$':
			if i+1 < len(command) && command[i+1] == '(' {
				return "command substitution $(...) is not allowed"
			}
		}
	}
	return ""
}

// Wrap is deprecated and no-op. The original intent was to prepend
// `ulimit -v ...; timeout 120 bash -c '...'` for sandbox-policy
// commands, but no caller ever wired it in (bash.go ran the raw
// command directly), so the timeout/ulimit was dead code that
// pretended to enforce limits. The bash tool now uses
// exec.CommandContext for timeout enforcement and ulimit-based
// memory caps were never load-bearing — keep this function as a
// noop for API compatibility with older callers.
func (s *Sandbox) Wrap(command string) string {
	return command
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

// matchPattern checks whether `cmd` matches `pattern`. Both are
// tokenized on whitespace; the pattern matches if every pattern token
// matches some cmd token in order, allowing arbitrary other cmd
// tokens between matches. A pattern token matches a cmd token if:
//
//   - they are exactly equal,
//   - the cmd token's basename equals the pattern token (so 'rm'
//     matches '/bin/rm' or 'sudo rm'),
//   - the cmd token starts with the pattern token (so 'if=' matches
//     'if=/dev/zero' and '/dev/sd' matches '/dev/sda'),
//   - the cmd token has the pattern token as a prefix segment (so
//     'mkfs' matches 'mkfs.ext4').
//
// The previous implementation was a raw substring containment which
// both missed obvious variants ('rm  -rf  /' with extra spaces, `rm
// --recursive --force`) AND false-positived on lines like
// `echo "confirmed"` where 'rm' happened to appear inside another
// word.
func matchPattern(cmd, pattern string) bool {
	cmdTokens := strings.Fields(cmd)
	patternTokens := strings.Fields(pattern)
	if len(patternTokens) == 0 || len(cmdTokens) == 0 {
		return false
	}
	ci := 0
	for _, pat := range patternTokens {
		found := false
		for ; ci < len(cmdTokens); ci++ {
			if cmdTokenMatches(cmdTokens[ci], pat) {
				found = true
				ci++
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// cmdTokenMatches reports whether a command-line token matches a
// pattern token under the rules described in matchPattern.
//
// Prefix matching is RESTRICTED so command-name patterns don't
// false-positive on longer command names. The rules:
//
//   - exact equality always matches
//   - basename equality matches ('rm' catches '/bin/rm')
//   - argument-style prefix ('if=', '/dev/sd') matches when patTok
//     contains '=' or '/' so 'if=' catches 'if=/dev/zero' and
//     '/dev/sd' catches '/dev/sda'
//   - versioned-binary prefix matches when the cmdTok char right
//     after patTok is '.' or '-' so 'mkfs' catches 'mkfs.ext4' and
//     'gpg' catches 'gpg-agent', but 'mv' does NOT catch 'mvn',
//     'cp' does NOT catch 'cpack', and 'sudo' does NOT catch
//     'sudoedit'
func cmdTokenMatches(cmdTok, patTok string) bool {
	if cmdTok == patTok {
		return true
	}
	if i := strings.LastIndex(cmdTok, "/"); i >= 0 {
		if cmdTok[i+1:] == patTok {
			return true
		}
	}
	if strings.ContainsAny(patTok, "=/") && strings.HasPrefix(cmdTok, patTok) {
		return true
	}
	if strings.HasPrefix(cmdTok, patTok) && len(cmdTok) > len(patTok) {
		next := cmdTok[len(patTok)]
		if next == '.' || next == '-' {
			return true
		}
	}
	return false
}

func truncateCmd(cmd string) string {
	if len(cmd) > 80 {
		return cmd[:80] + "..."
	}
	return cmd
}

