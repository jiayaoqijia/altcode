---
description: Analyze Go import dependencies for a package
allowed-tools: Bash, Read, Grep
---

Analyze the Go import dependencies for the package at $ARGUMENTS.

Steps:
1. Read the Go files in the package
2. List all imports
3. Categorize as: stdlib, internal, external
4. Report any circular dependency risks
