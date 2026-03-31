---
name: prepare-pr
description: Prepare a GitHub PR for merge by rebasing onto main, fixing review findings, running gates, committing fixes, and pushing to the PR head branch. Use after /review-pr. Never merge or push to main.
---

# Prepare PR

## Overview

Prepare a PR branch for merge with review fixes, green gates, and an updated head branch.

## Inputs

- Ask for PR number or URL.
- If missing, auto-detect from conversation.
- If ambiguous, ask.

## Safety

- Never push to `main` or `origin/main`. Push only to the PR head branch.
- Never run `git push` without specifying remote and branch explicitly. Do not run bare `git push`.
- Do not run `git clean -fdx`.
- Do not run `git add -A` or `git add .`. Stage only specific files changed.

## Execution Rule

- Execute the workflow. Do not stop after printing the TODO checklist.
- If delegating, require the delegate to run commands and capture outputs.

## Completion Criteria

- Rebase PR commits onto `origin/main`.
- Fix all BLOCKER and IMPORTANT items from `.local/review.md`.
- Run gates and pass.
- Commit prep changes.
- Push the updated HEAD back to the PR head branch.
- Write `.local/prep.md` with a prep summary.
- Output exactly: `PR is ready for /merge-pr`.

## First: Create a TODO Checklist

Create a checklist of all prep steps, print it, then continue and execute the commands.

## Setup: Use a Worktree

Use an isolated worktree for all prep work.

```sh
git rev-parse --show-toplevel
WORKTREE_DIR=".worktrees/pr-<PR>"
```

Run all commands inside the worktree directory.

## Load Review Findings (Mandatory)

```sh
if [ -f .local/review.md ]; then
  echo "Found review findings from /review-pr"
else
  echo "Missing .local/review.md. Run /review-pr first and save findings."
  exit 1
fi

sed -n '1,200p' .local/review.md
```

## Steps

1. Identify PR meta (author, head branch, head repo URL)

```sh
gh pr view <PR> --json number,title,author,headRefName,baseRefName,headRepository,body --jq '{number,title,author:.author.login,head:.headRefName,base:.baseRefName,headRepo:.headRepository.nameWithOwner,body}'
contrib=$(gh pr view <PR> --json author --jq .author.login)
head=$(gh pr view <PR> --json headRefName --jq .headRefName)
head_repo_url=$(gh pr view <PR> --json headRepository --jq .headRepository.url)
```

2. Fetch the PR branch tip into a local ref

```sh
git fetch origin pull/<PR>/head:pr-<PR>
```

3. Rebase PR commits onto latest main

```sh
git reset --hard pr-<PR>
git fetch origin main
git rebase origin/main
```

If conflicts happen: resolve each file, `git add <file>`, `git rebase --continue`.
If you resolve conflicts 3+ times, stop and report.

4. Fix issues from `.local/review.md`

- Fix all BLOCKER and IMPORTANT items.
- NITs are optional.
- Keep scope tight.

Keep a running log in `.local/prep.md`.

5. Update `CHANGELOG.md` if flagged in review

6. Update docs if flagged in review

7. Commit prep fixes

Stage only specific files:

```sh
git add <file1> <file2> ...
git commit -m "fix: <summary> (#<PR>) (thanks @$contrib)"
```

8. Run full gates before pushing

Run build, lint, and test commands for the project. Require all to pass.
Allow at most 3 fix-and-rerun cycles. If gates still fail after 3 attempts, stop and report.

9. Push updates back to the PR head branch

```sh
git remote add prhead "$head_repo_url.git" 2>/dev/null || git remote set-url prhead "$head_repo_url.git"

echo "Pushing to branch: $head"
if [ "$head" = "main" ] || [ "$head" = "master" ]; then
  echo "ERROR: head branch is main/master. Stopping."
  exit 1
fi
git push --force-with-lease prhead HEAD:$head
```

10. Verify PR is not behind main

```sh
git fetch origin main
git fetch origin pull/<PR>/head:pr-<PR>-verify --force
git merge-base --is-ancestor origin/main pr-<PR>-verify && echo "PR is up to date with main" || echo "ERROR: PR is still behind main, rebase again"
git branch -D pr-<PR>-verify 2>/dev/null || true
```

11. Write prep summary to `.local/prep.md`

12. Output diff stat and print: `PR is ready for /merge-pr`

## Guardrails

- Worktree only.
- Do not delete the worktree on success. `/merge-pr` may reuse it.
- Do not run `gh pr merge`.
- Never push to main. Only push to the PR head branch.
- Run and pass all gates before pushing.
