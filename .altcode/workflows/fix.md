---
name: fix
description: Diagnose, fix, and verify a bug
phases:
  - name: diagnose
    agents:
      - role: investigator
        backend: claude
        prompt: "Investigate and diagnose the root cause: {{.Task}}"
    timeout: 5m
    required: true
  - name: fix
    depends_on: [diagnose]
    agents:
      - role: fixer
        backend: codex
        prompt: "Fix the bug and write a regression test: {{.Task}}"
    timeout: 15m
    required: true
  - name: verify
    depends_on: [fix]
    agents:
      - role: verifier
        backend: claude
        prompt: "Run tests and verify the fix is correct. Check for regressions."
    timeout: 5m
---
Bug fix workflow: diagnose, fix, verify.
