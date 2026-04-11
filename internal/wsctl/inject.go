package wsctl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// maxContextSize caps the injected context.md content to 32KB.
const maxContextSize = 32 * 1024

// InjectWorkspaceContext writes the shared workspace context into the
// agent's native instruction file. Claude backends get CLAUDE.md;
// codex/opencode/aider get AGENTS.md.
func InjectWorkspaceContext(
	workDir string,
	provider string,
	role string,
	task string,
	sess *workspace.WorkspaceSession,
) error {
	content := buildContent(role, task, sess)

	filename := targetFile(provider)
	path := filepath.Join(workDir, filename)

	existing, _ := os.ReadFile(path)
	merged := string(existing)
	if len(merged) > 0 && !strings.HasSuffix(merged, "\n") {
		merged += "\n"
	}
	merged += "\n" + content

	// Scan FULL merged content (not just new block) before writing
	if err := SecretGuard(merged); err != nil {
		return fmt.Errorf("secret guard: %w", err)
	}

	return os.WriteFile(path, []byte(merged), 0o644)
}

// targetFile returns the context file name for the given backend.
func targetFile(provider string) string {
	if provider == "claude" {
		return "CLAUDE.md"
	}
	return "AGENTS.md"
}

// buildContent assembles the workspace context block.
func buildContent(
	role, task string,
	sess *workspace.WorkspaceSession,
) string {
	var b strings.Builder
	b.WriteString("# Workspace Context\n\n")
	b.WriteString(fmt.Sprintf("**Workspace:** %s\n", sess.ID))
	b.WriteString(fmt.Sprintf("**Task:** %s\n", task))
	b.WriteString(fmt.Sprintf("**Your Role:** %s\n", role))
	b.WriteString(fmt.Sprintf("**Status:** %s\n", sess.Status))
	b.WriteString(fmt.Sprintf("**Updated:** %s\n\n",
		time.Now().UTC().Format(time.RFC3339)))

	b.WriteString(agentTable(sess))

	ctx := readSharedContext(sess)
	if ctx != "" {
		b.WriteString("\n## Shared Context\n\n")
		b.WriteString(ctx)
		b.WriteString("\n")
	}

	return b.String()
}

// agentTable renders a markdown table of all agents' status.
//
// WorkspaceSession.Agents is a map guarded by sess.mu — iterating it
// without the lock races with writers in workspace_run.go (spawn/exit
// goroutines). Lock for the duration of the render so 'concurrent
// map iteration and map write' can't panic the process.
func agentTable(sess *workspace.WorkspaceSession) string {
	var b strings.Builder
	b.WriteString("## Workspace Agents Status\n\n")
	b.WriteString("| Role | Branch | PR | CI | Activity |\n")
	b.WriteString("|------|--------|----|----|----------|\n")
	sess.Lock()
	defer sess.Unlock()
	for _, rec := range sess.Agents {
		pr := "--"
		if rec.PRID > 0 {
			pr = fmt.Sprintf("PR#%d", rec.PRID)
		}
		ci := string(rec.CIStatus)
		if ci == "" {
			ci = "--"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			rec.Role, rec.Branch, pr, ci, rec.ActivityState))
	}
	return b.String()
}

// readSharedContext reads the workspace's context.md, capped at
// maxContextSize bytes. Returns empty string on any error.
func readSharedContext(sess *workspace.WorkspaceSession) string {
	if sess.ID == "" || sess.GitRoot == "" {
		return ""
	}
	// Use absolute path from sess.GitRoot, not relative CWD
	ctxDir := filepath.Join(sess.GitRoot, ".altcode", "workspace", sess.ID)
	data, err := os.ReadFile(filepath.Join(ctxDir, "context.md"))
	if err != nil {
		return ""
	}
	s := string(data)
	if len(s) > maxContextSize {
		s = s[:maxContextSize] + "\n[truncated]\n"
	}
	return s
}

// secretPatterns are regex patterns that match common API key formats.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`gho_[A-Za-z0-9]{36,}`),
	regexp.MustCompile(`AKIA[A-Z0-9]{16,}`),
	regexp.MustCompile(`xoxb-[0-9A-Za-z-]+`),
	regexp.MustCompile(`xoxp-[0-9A-Za-z-]+`),
	regexp.MustCompile(`-----BEGIN (RSA |EC )?PRIVATE KEY-----`),
}

// SecretGuard scans content for known API key patterns. Returns an
// error describing the first match found, nil if clean.
func SecretGuard(content string) error {
	for _, re := range secretPatterns {
		if loc := re.FindStringIndex(content); loc != nil {
			sample := content[loc[0]:loc[1]]
			if len(sample) > 12 {
				sample = sample[:12] + "..."
			}
			return fmt.Errorf(
				"secret detected (pattern %s): %q",
				re.String(), sample)
		}
	}
	return nil
}
