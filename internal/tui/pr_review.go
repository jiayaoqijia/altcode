package tui

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type prReference struct {
	owner  string
	repo   string
	number int
	raw    string
}

type prCheckoutRunner func(context.Context, string, string, ...string) error

var runPRCheckoutCommand prCheckoutRunner = execPRCheckoutCommand

func parsePRReference(raw string) (prReference, bool) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "<>")
	if trimmed == "" {
		return prReference{}, false
	}

	if u, err := url.Parse(trimmed); err == nil && strings.EqualFold(u.Host, "github.com") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull" {
			n, err := strconv.Atoi(parts[3])
			if err == nil && n > 0 {
				return prReference{
					owner:  parts[0],
					repo:   parts[1],
					number: n,
					raw:    trimmed,
				}, true
			}
		}
	}

	if n, err := strconv.Atoi(strings.TrimPrefix(trimmed, "#")); err == nil && n > 0 {
		return prReference{number: n, raw: trimmed}, true
	}

	if left, right, ok := strings.Cut(trimmed, "#"); ok {
		repoParts := strings.Split(left, "/")
		n, err := strconv.Atoi(right)
		if len(repoParts) == 2 && err == nil && n > 0 {
			return prReference{
				owner:  repoParts[0],
				repo:   repoParts[1],
				number: n,
				raw:    trimmed,
			}, true
		}
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) == 4 && parts[2] == "pull" {
		n, err := strconv.Atoi(parts[3])
		if err == nil && n > 0 {
			return prReference{
				owner:  parts[0],
				repo:   parts[1],
				number: n,
				raw:    trimmed,
			}, true
		}
	}

	return prReference{}, false
}

func (a *App) preparePRReviewPrompt(raw string) (string, string, error) {
	ref, ok := parsePRReference(raw)
	if !ok {
		return "", "", fmt.Errorf("not a GitHub PR reference: %s", raw)
	}
	root, err := preparePRReviewCheckout(context.Background(), a.projectRoot, ref, runPRCheckoutCommand)
	if err != nil {
		return "", "", err
	}
	a.setProjectRoot(root)

	target := ref.raw
	if target == "" || target == strconv.Itoa(ref.number) || target == "#"+strconv.Itoa(ref.number) {
		target = fmt.Sprintf("#%d", ref.number)
	}
	prompt := fmt.Sprintf(
		"Review %s in the dedicated checkout at %s. "+
			"Use file tools only inside that checkout. Start with `gh pr view %s` for metadata, "+
			"then inspect the checked-out files and diff for bugs, security issues, and code quality. "+
			"Be terse. Tag findings: BLOCKER / HIGH / MEDIUM / NIT.",
		target, root, target)
	info := fmt.Sprintf("[review] scoped file tools to PR checkout: %s", root)
	return prompt, info, nil
}

func (a *App) setProjectRoot(root string) {
	a.projectRoot = root
	if a.tools != nil {
		a.tools.projectRoot = root
	}
	if a.engine != nil {
		a.engine.SetProjectRoot(root)
	}
}

func preparePRReviewCheckout(ctx context.Context, currentRoot string, ref prReference, run prCheckoutRunner) (string, error) {
	if run == nil {
		run = execPRCheckoutCommand
	}
	ctx, cancel := prCheckoutContext(ctx)
	defer cancel()

	if ref.owner != "" && ref.repo != "" {
		return preparePRReviewClone(ctx, ref, run)
	}
	return preparePRReviewWorktree(ctx, currentRoot, ref, run)
}

func preparePRReviewClone(ctx context.Context, ref prReference, run prCheckoutRunner) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}
	checkout := filepath.Join(cacheDir, "altcode", "pr-reviews", ref.owner, ref.repo, fmt.Sprintf("pr-%d", ref.number))
	if !isGitCheckout(checkout) {
		if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
			return "", fmt.Errorf("create PR checkout parent: %w", err)
		}
		if err := run(ctx, "", "gh", "repo", "clone", ref.owner+"/"+ref.repo, checkout, "--", "--filter=blob:none"); err != nil {
			return "", err
		}
	} else if err := run(ctx, checkout, "git", "fetch", "origin", "main"); err != nil {
		return "", err
	}
	if err := run(ctx, checkout, "git", "fetch", "origin", fmt.Sprintf("pull/%d/head", ref.number)); err != nil {
		return "", err
	}
	if err := run(ctx, checkout, "git", "checkout", "-B", fmt.Sprintf("altcode/pr-%d", ref.number), "FETCH_HEAD"); err != nil {
		return "", err
	}
	return checkout, nil
}

func preparePRReviewWorktree(ctx context.Context, currentRoot string, ref prReference, run prCheckoutRunner) (string, error) {
	if currentRoot == "" || !isGitCheckout(currentRoot) {
		return "", fmt.Errorf("PR number review needs to run from a git repository or use a full GitHub PR URL")
	}
	checkout := filepath.Join(currentRoot, ".worktrees", fmt.Sprintf("pr-%d", ref.number))
	branch := fmt.Sprintf("altcode/pr-%d", ref.number)
	if err := run(ctx, currentRoot, "git", "fetch", "origin", "main"); err != nil {
		return "", err
	}
	if !isGitCheckout(checkout) {
		if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
			return "", fmt.Errorf("create PR worktree parent: %w", err)
		}
		if err := run(ctx, currentRoot, "git", "worktree", "add", "-B", branch, checkout, "origin/main"); err != nil {
			return "", err
		}
	} else if err := run(ctx, checkout, "git", "checkout", branch); err != nil {
		return "", err
	}
	if err := run(ctx, checkout, "git", "fetch", "origin", fmt.Sprintf("pull/%d/head", ref.number)); err != nil {
		return "", err
	}
	if err := run(ctx, checkout, "git", "reset", "--hard", "FETCH_HEAD"); err != nil {
		return "", err
	}
	return checkout, nil
}

func prCheckoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 2*time.Minute)
}

func execPRCheckoutCommand(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
}

func isGitCheckout(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}
