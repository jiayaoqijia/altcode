---
name: review
description: Parallel adversarial code review
phases:
  - name: review
    parallel: true
    agents:
      - role: reviewer
        backend: claude
        prompt: "Review the codebase for bugs, security issues, and design problems: {{.Task}}"
      - role: challenger
        backend: codex
        prompt: "Be adversarial. Find race conditions, edge cases, missing tests: {{.Task}}"
    timeout: 10m
---
Parallel code review with two independent reviewers.
