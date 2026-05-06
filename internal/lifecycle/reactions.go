package lifecycle

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

const maxCILogBytes = 8192

// DispatchCIFix routes CI failure output to the primary agent,
// asking it to fix the failing checks and push a new commit.
func DispatchCIFix(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
	ciLog string,
	store *workspace.Store,
) error {
	if sess.CIRetries >= sess.MaxCIRetries {
		return fmt.Errorf(
			"ci fix: retry limit %d reached", sess.MaxCIRetries)
	}
	primary := primaryAgent(sess)
	if primary == nil {
		return fmt.Errorf("ci fix: no primary agent in session")
	}
	log := truncateLog(ciLog, maxCILogBytes)
	prompt := buildCIFixPrompt(
		primary.Role, sess.Task, log,
		sess.CIRetries+1, sess.MaxCIRetries,
	)
	primary.ActivityState = workspace.ActivitySpawning
	if err := store.SendMessage(ctx, sess.ID, primary.Role, prompt); err != nil {
		return fmt.Errorf("ci fix: send message: %w", err)
	}
	// Only increment AFTER successful send — prevents eating retries on transient errors
	sess.CIRetries++
	return nil
}

// RestartAgent kills and relaunches an agent with --resume.
func RestartAgent(
	ctx context.Context,
	rec *workspace.AgentRecord,
	sess *workspace.WorkspaceSession,
	plugins workspace.PluginSet,
) error {
	if rec.RestartCount >= MaxRestarts {
		return fmt.Errorf(
			"agent %s: restart limit reached", rec.Role)
	}
	rec.RestartCount++
	agentPlugin, ok := plugins.Agents[rec.Backend]
	if !ok {
		return fmt.Errorf(
			"agent %s: unknown backend %q", rec.Role, rec.Backend)
	}
	agentSess := buildAgentSession(rec, sess)
	// Merge per-agent env vars (ALTCODE_SESSION_ID, ALTCODE_ROLE, CODEX_HOME, etc.)
	extra, _ := agentPlugin.Environment(agentSess)
	for k, v := range extra {
		agentSess.Env = append(agentSess.Env, k+"="+v)
	}
	cmd, err := agentPlugin.RestoreCommand(agentSess)
	if cmd == nil || err != nil {
		cmd, err = agentPlugin.LaunchCommand(agentSess)
	}
	if err != nil {
		return fmt.Errorf("agent %s: launch: %w", rec.Role, err)
	}
	handle, err := plugins.Runtime.Spawn(
		ctx, cmd, agentSess.Env, rec.WorktreePath)
	if err != nil {
		return fmt.Errorf("agent %s: spawn: %w", rec.Role, err)
	}
	rec.RuntimeHandleID = handle.ID
	rec.ActivityState = workspace.ActivitySpawning
	return nil
}

// ExtractReviewContext formats review comments for agent injection.
func ExtractReviewContext(
	reviews []*workspace.Review,
	taskDesc string,
) string {
	byFile := map[string][]workspace.ReviewComment{}
	for _, r := range reviews {
		for _, c := range r.Comments {
			byFile[c.Path] = append(byFile[c.Path], c)
		}
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)

	var b strings.Builder
	b.WriteString(
		"PR Review Feedback — please address these comments:\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "**File: %s**\n", f)
		comments := byFile[f]
		sort.Slice(comments, func(i, j int) bool {
			return comments[i].Line < comments[j].Line
		})
		for _, c := range comments {
			fmt.Fprintf(&b, "  Line %d: %q\n", c.Line, c.Body)
		}
		b.WriteString("\n")
	}
	b.WriteString("After addressing all comments:\n")
	b.WriteString("1. Commit your changes.\n")
	b.WriteString("2. Push to your branch.\n")
	b.WriteString(
		"3. The reviewer will be automatically re-requested.\n")
	return b.String()
}

// primaryAgent returns the implementer or first agent.
func primaryAgent(
	sess *workspace.WorkspaceSession,
) *workspace.AgentRecord {
	for _, rec := range sess.Agents {
		if rec.Role == "implementer" {
			return rec
		}
	}
	for _, rec := range sess.Agents {
		return rec
	}
	return nil
}

func buildCIFixPrompt(
	role, task, ciLog string,
	attempt, max int,
) string {
	return fmt.Sprintf(`CI checks failed on your PR (attempt %d/%d).

Original task: %s

CI failure output:
%s

Please:
1. Identify the root cause of the failure.
2. Fix the failing checks.
3. Run the checks locally to verify.
4. Commit the fix and push to your branch.

Do not create a new PR — push to the existing branch.`,
		attempt, max, task, ciLog)
}

func truncateLog(log string, max int) string {
	if len(log) <= max {
		return log
	}
	return log[:max] + "\n\n[log truncated]"
}
