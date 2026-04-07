---
name: ship-feature
description: Design, implement, and review a feature end-to-end
phases:
  - name: design
    agents:
      - role: architect
        backend: claude
        prompt: |
          Read the codebase. Design the implementation for: {{.Task}}
          Output a concrete plan with files to create/modify.
    timeout: 10m
    required: true
  - name: implement
    depends_on: [design]
    agents:
      - role: implementer
        backend: codex
        prompt: "Implement the feature: {{.Task}}"
    timeout: 20m
    required: true
  - name: review
    depends_on: [implement]
    parallel: true
    on_failure: human
    agents:
      - role: reviewer
        backend: claude
        prompt: "Review the implementation for bugs, security issues, and code quality."
      - role: challenger
        backend: codex
        prompt: "Find race conditions, edge cases, and missing error handling."
---
End-to-end feature development: design, implement, adversarial review.
