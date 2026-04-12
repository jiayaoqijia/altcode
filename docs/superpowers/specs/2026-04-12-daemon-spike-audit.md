# Spike: Daemon Integration API Surface Audit

> **Deliverable for Issue #9** — prerequisite for all AltFix daemon work (Issues #3-#8)

## Executive Summary

**3 of 5 internal packages are daemon-ready with ZERO refactoring.** The remaining 2 need ~150 lines of adaptation total. The HTTP daemon wrapper is estimated at 200-300 lines (revised DOWN from the original 500-line estimate).

## Package-by-Package Audit

### 1. `internal/workspace/` — 🟡 Needs ~100 lines

**Exported API (daemon-relevant):**
- `WorkspaceSession` — fully JSON-serializable, tracks ID/Task/Status/Agents/GitRoot
- `Store` — `SaveSession()`, `LoadSession()`, `ListSessions()`, `AppendActivity()`, `SendMessage()`
- `EventLog` — `Emit()`, `GetEvents()`, `Tail()` for durable event streaming
- `NewWorktreeWorkspace()` — worktree isolation (git worktree add/remove)
- `WorkspaceSetupRequest` / `WorkspaceSetupResult` — programmatic workspace init

**Init requirements:**
- Git root detection via `config.DetectProjectRoot`
- Backend loading via `backends.LoadAgentDefsIntoRegistry`
- Store path at `.altcode/workspace/{id}`

**TUI dependency:** cmd/altcode/workspace.go uses `fmt.Printf` for terminal output streaming. The packages themselves are clean.

**Refactoring needed:**
- Add an `OutputSink` interface (callback for agent output instead of fmt.Printf)
- Move signal.NotifyContext from workspace.go into CLI wrapper
- EventLog is already daemon-ready (JSONL-based, queryable)
- **Effort: ~100 lines**

---

### 2. `internal/orchestrator/` — ✅ Daemon-Ready (0 lines)

**Exported API:**
- `NewSession()` / `NewSessionFromConfig()` — create multi-model session
- `RunParallel(ctx, prompt)` → `[]Finding` — parallel model invocation
- `CrossCheck(ctx)` → `[]Finding` — cross-validation
- `Synthesize()` → `*Verdict` — decision aggregation
- `Findings()` → `[]Finding`
- Role enum: architect, implementer, reviewer, challenger, evaluator

**TUI dependency:** NONE. Pure business logic, returns structured data.

**Refactoring needed:** None.

---

### 3. `internal/scm/` — ✅ Daemon-Ready (0 lines)

**Exported API (via `workspace.SCM` interface):**
- `CreatePR(ctx, CreatePRRequest)` → `*PR`
- `GetPR(ctx, branch)` → `*PR`
- `ListPRs(ctx)` → `[]*PR`
- `GetPRReviews(ctx, prNumber)` → `[]*Review`
- `RequestReview(ctx, prNumber, reviewers)`
- `MergePR(ctx, prNumber, method)`
- `CIStatus(ctx, sha)` → `CICheckStatus`
- `CILogs(ctx, sha)` → string

**Init requirements:** GitHub token from env/config, repo owner/name detection.

**TUI dependency:** NONE. Shells out to `gh` CLI, no terminal control.

**Refactoring needed:** None. Consider adding `NewGitHubSCMWithOwnerRepo(owner, repo)` for explicit construction without auto-detection.

---

### 4. `internal/memory/` — ✅ Daemon-Ready (0 lines)

**Exported API:**
- `NewStore(dir)` — create store
- `Save(id, title, content)` / `Load(id)` / `List()` / `Delete(id)` / `Search(query)`
- `ForContext(maxBytes)` → string (summary for agent injection)
- `LoadIndex()` → string (MEMORY.md content)

**Init requirements:** Directory path only. Atomic writes, mutex-protected.

**TUI dependency:** NONE. Pure file I/O.

**Refactoring needed:** None.

---

### 5. `internal/workspace/backends/` — 🟡 Needs ~50 lines

**Exported API:**
- `Agent` interface (7 methods): `LaunchCommand`, `RestoreCommand`, `Environment`, `ActivityState`, `IsProcessRunning`, `SessionInfo`, `SetupWorkspaceHooks`, `Name`
- Implementations: `ClaudeBackend`, `CodexBackend`, `AiderBackend`, `OpenCodeBackend`
- `Registry` for name → backend lookup
- `ActivityDetection` for state polling (JSONL parsing)

**Init requirements:** Backend path detection (which claude/codex/etc.), optional version check.

**TUI dependency:** LaunchCommand returns argv; the CLI layer decides whether to spawn with tmux (PTY) or process mode. The backends themselves don't assume a terminal.

**Refactoring needed:**
- Daemon must always use process runtime (never tmux)
- ActivityState() is the daemon's polling mechanism (replaces terminal output streaming)
- **Effort: ~50 lines** for a DaemonRuntime adapter or a flag to skip tmux detection

---

## Init Sequence for Daemon

```go
// 1. Load config
cfg := config.Default()
config.MergeFrom(cfg, "/path/to/config.json")
auth.LoadFromCLIs(cfg)

// 2. Detect project
projectRoot := config.DetectProjectRoot(repoDir)
instructions, _ := config.LoadInstructions(projectRoot)

// 3. Init subsystems
memStore := memory.NewStore(memory.DefaultDir(projectRoot))
scm := scm.NewGitHubSCM()  // auto-detect
wsStore := workspace.NewStore(filepath.Join(projectRoot, ".altcode", "workspace"))

// 4. Load backends
backends := backends.DetectBackends()
backends.LoadAgentDefsIntoRegistry(projectRoot)

// 5. Ready for HTTP handlers
```

## Estimate Validation

| Component | Original estimate | Revised estimate | Reason |
|-----------|------------------|-----------------|--------|
| HTTP daemon (6 endpoints) | ~300 lines | ~200 lines | 3/5 packages need zero wrapping |
| SSE streaming | ~100 lines | ~80 lines | EventLog.Tail() already exists |
| Task persistence | ~100 lines | ~60 lines | workspace.Store already has SQLite patterns |
| Steering | ~50 lines | ~30 lines | workspace.Store.SendMessage() exists |
| OutputSink refactor | (not estimated) | ~100 lines | New — decouple fmt.Printf from workspace.go |
| Daemon runtime | (not estimated) | ~50 lines | New — process-mode spawn without tmux |
| **Total** | **~550 lines** | **~520 lines** | Roughly matches — estimate validated |

## Blockers

1. **Backend PATH access:** daemon must run with the same environment as the CLI or be explicitly told which backends are available.
2. **Git repo detection:** can fail if daemon runs outside a repo. Must pass `gitRoot` explicitly in the task payload.
3. **Concurrent workspace limit:** Store doesn't enforce a max. Multiple simultaneous tasks could collide. Use ULID-based IDs (already the case) and add a semaphore if needed.
4. **No concurrent workspace limit in Store:** multiple daemon instances could collide on session IDs. Existing ULID generation should prevent this but hasn't been tested under concurrency.

## Recommendation

**Proceed with Issue #3 (daemon)**. The spike confirms the ~500-line estimate is valid. The OutputSink refactor (#1 blocker) should be the first commit of the daemon implementation, not a separate PR, because it touches the same cmd/altcode/workspace.go file the daemon will wrap.
