---
name: merge-pr
description: Merge a GitHub PR via squash after /prepare-pr. Use when asked to merge a ready PR. Do not push to main or modify code. Ensure the PR ends in MERGED state and clean up worktrees after success.
---

# Merge PR

## Overview

Merge a prepared PR via `gh pr merge --squash` and clean up the worktree after success.

## Inputs

- Ask for PR number or URL.
- If missing, auto-detect from conversation.
- If ambiguous, ask.

## Safety

- Use `gh pr merge --squash` as the only path to `main`.
- Do not run `git push` at all during merge.

## Execution Rule

- Execute the workflow. Do not stop after printing the TODO checklist.
- If delegating, require the delegate to run commands and capture outputs.

## Completion Criteria

- Ensure `gh pr merge` succeeds.
- Ensure PR state is `MERGED`, never `CLOSED`.
- Record the merge SHA.
- Run cleanup only after merge success.

## First: Create a TODO Checklist

Create a checklist of all merge steps, print it, then continue and execute the commands.

## Setup: Use a Worktree

```sh
git rev-parse --show-toplevel
WORKTREE_DIR=".worktrees/pr-<PR>"
```

Run all commands inside the worktree directory.

## Load Local Artifacts (Mandatory)

Expect these files from earlier steps:

- `.local/review.md` from `/review-pr`
- `.local/prep.md` from `/prepare-pr`

```sh
ls -la .local || true

if [ -f .local/review.md ]; then
  echo "Found .local/review.md"
  sed -n '1,120p' .local/review.md
else
  echo "Missing .local/review.md. Run /review-pr, then /prepare-pr."
  exit 1
fi

if [ -f .local/prep.md ]; then
  echo "Found .local/prep.md"
  sed -n '1,120p' .local/prep.md
else
  echo "Missing .local/prep.md. Run /prepare-pr first."
  exit 1
fi
```

## Steps

1. Identify PR meta

```sh
gh pr view <PR> --json number,title,state,isDraft,author,headRefName,baseRefName,headRepository,body --jq '{number,title,state,isDraft,author:.author.login,head:.headRefName,base:.baseRefName,headRepo:.headRepository.nameWithOwner,body}'
contrib=$(gh pr view <PR> --json author --jq .author.login)
head=$(gh pr view <PR> --json headRefName --jq .headRefName)
```

2. Run sanity checks

Stop if any are true: PR is a draft, required checks are failing, branch is behind main.

```sh
gh pr checks <PR>

git fetch origin main
git fetch origin pull/<PR>/head:pr-<PR>
git merge-base --is-ancestor origin/main pr-<PR> || echo "PR branch is behind main, run /prepare-pr"
```

3. Merge PR and delete branch

```sh
check_status=$(gh pr checks <PR> 2>&1)
if echo "$check_status" | grep -q "pending\|queued"; then
  echo "Checks still running, using --auto to queue merge"
  gh pr merge <PR> --squash --delete-branch --auto
  echo "Merge queued. Monitor with: gh pr checks <PR> --watch"
else
  gh pr merge <PR> --squash --delete-branch
fi
```

If merge fails, report the error and stop. Do not retry in a loop.

4. Get merge SHA

```sh
merge_sha=$(gh pr view <PR> --json mergeCommit --jq '.mergeCommit.oid')
echo "merge_sha=$merge_sha"
```

5. Optional comment

```sh
gh pr comment <PR> -F - <<'EOF'
Merged via squash.

- Merge commit: $merge_sha

Thanks @$contrib!
EOF
```

6. Verify PR state is MERGED

```sh
gh pr view <PR> --json state --jq .state
```

7. Clean up worktree only on success

Run cleanup only if step 6 returned `MERGED`.

```sh
git worktree remove ".worktrees/pr-<PR>" --force
git branch -D temp/pr-<PR> 2>/dev/null || true
git branch -D pr-<PR> 2>/dev/null || true
```

## Guardrails

- Worktree only.
- Do not close PRs.
- End in MERGED state.
- Clean up only after merge success.
- Never push to main. Use `gh pr merge --squash` only.
- Do not run `git push` at all in this command.
