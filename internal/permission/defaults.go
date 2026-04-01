package permission

// DefaultRules returns the built-in permission rules.
func DefaultRules() []Rule {
	return []Rule{
		// Read-only tools are always allowed
		{Tool: "read", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "glob", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "grep", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "ls", Pattern: "*", Action: ActionAllow, Source: "default"},
		{Tool: "fetch", Pattern: "*", Action: ActionAllow, Source: "default"},

		// Safe git commands
		{Tool: "bash", Pattern: "git status", Action: ActionAllow, Source: "default"},
		{Tool: "bash", Pattern: "git diff *", Action: ActionAllow, Source: "default"},
		{Tool: "bash", Pattern: "git log *", Action: ActionAllow, Source: "default"},
	}
}
