---
name: code-analyzer
description: Analyze code quality metrics for a Go package
allowed-tools:
  - Bash
  - Read
  - Grep
  - Glob
---

Analyze the Go package at the path provided.

Report:
1. Number of Go files
2. Total lines of code
3. Number of exported functions
4. Number of test files
5. Test coverage estimate (files with tests / total files)
