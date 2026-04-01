package command

// Command is a slash command loaded from a markdown file.
type Command struct {
	Name         string   // derived from filename (without .md)
	Description  string   // from frontmatter
	ArgumentHint string   // from frontmatter
	AllowedTools []string // from frontmatter
	Body         string   // markdown body after frontmatter
	Path         string   // source file path
}
