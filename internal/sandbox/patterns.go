package sandbox

// defaultSafeBlockList returns patterns blocked in PolicySafe mode.
//
// Patterns are matched token-by-token by sandbox.matchPattern, with
// the first token compared against the basename of the cmd token —
// so 'rm' catches '/bin/rm' or 'sudo rm', and 'sudo' catches
// '/usr/bin/sudo'. Whitespace inside a pattern is split into tokens.
func defaultSafeBlockList() []string {
	return []string{
		"rm -rf /",
		"rm -rf ~",
		"rm -rf .",
		"rm -fr /",
		"rm --recursive --force",
		"dd if=",
		"mkfs",
		"fdisk",
		":(){ :|:& };:",
		"> /dev/sd",
		"chmod -R 777 /",
		// Privilege escalation
		"sudo",
		"doas",
		// Code-eval primitives
		"eval",
		// Pipe-to-shell foot guns. matchPattern checks tokens in order
		// so "curl | sh" matches "curl https://x | sh" and similar.
		"curl | sh",
		"curl | bash",
		"wget | sh",
		"wget | bash",
		"| sh",
		"| bash",
	}
}

// defaultReadOnlyBlockList blocks write operations.
func defaultReadOnlyBlockList() []string {
	list := defaultSafeBlockList()
	list = append(list,
		"rm",
		"mv",
		"cp",
		"mkdir",
		"rmdir",
		"touch",
		"chmod",
		"chown",
		"tee",
		"git push",
		"git commit",
		"git reset",
	)
	return list
}
