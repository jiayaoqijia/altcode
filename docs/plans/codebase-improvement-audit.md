# Codebase Improvement Audit

## Highest-impact improvements

1. Remove committed/local secret material immediately.
   - A root `.env` file exists and contains live-looking provider keys.
   - Treat these credentials as compromised: revoke/rotate them, remove the file from the repo/worktree, and add a CI secret scan gate.

2. Delete generated/demo packages from production tree.
   - Stray packages such as `internal/math`, `internal/fib`, `internal/prime`, `internal/cache`, root `lru/`, root `middleware/`, `cmd/hello`, `cmd/cli`, `cmd/cli-example`, and `cmd/flagcli` look model-generated or tutorial code.
   - These increase build/test surface and contradict the repo's own pre-push cleanup rule.

3. Harden external editor launch.
   - `internal/tui/external_editor.go` builds `sh -c editor + " " + tmpPath` from `$VISUAL`/`$EDITOR`.
   - Prefer splitting editor command safely or explicit shell policy; at minimum quote the temp path. Current behavior enables surprising shell execution via editor env values.

4. Reduce `cmd/altcode/main.go` complexity.
   - The root Cobra setup and exec/TUI wiring are concentrated in one very large file.
   - Split flag registration, print-and-exit commands, config loading, exec translation, and TUI startup into focused files/packages.

5. Reconcile database driver choices.
   - The module depends on both `github.com/mattn/go-sqlite3` and `modernc.org/sqlite`, but active stores import only `modernc.org/sqlite`.
   - Remove unused CGO sqlite dependency if truly unnecessary; this improves cross-platform builds.

6. Replace sleep-heavy tests with condition-based waits.
   - Several tests use fixed `time.Sleep`, especially daemon/TUI/oauth/pubsub tests.
   - Prefer polling with deadline, channels, or test hooks to reduce flakiness and speed up CI.

7. Add mechanical anti-junk and secret gates.
   - The repo documentation lists cleanup commands, but enforcement appears prose-based.
   - Add CI/pre-commit checks for forbidden generated paths and secret patterns.

8. Review command expansion trust boundary.
   - `internal/command/expand.go` supports `!\`cmd\`` shell expansion from slash-command markdown.
   - Args are escaped, but the command body itself is shell-executed. Gate this behind explicit trusted command sources and document that marketplace/user commands can execute local shell.

9. Normalize package surface.
   - There are duplicate middleware packages under root and `pkg/`, plus unrelated sample binaries.
   - Keep only supported public APIs under `pkg/`; move or remove everything else.

10. Expand security tests around hooks and gateways.
   - HTTP hooks fail closed, which is good.
   - Add SSRF/loopback policy tests if hooks can be configured from untrusted or shared config, and finish the DingTalk signature TODO before production exposure.

## Suggested order

1. Secret rotation + remove `.env`.
2. Delete generated/demo packages; run `GOFLAGS=-mod=mod go test ./...`.
3. Add CI checks preventing reintroduction.
4. Refactor `cmd/altcode/main.go` incrementally.
5. Harden editor launch and command expansion policy.
6. Flake-reduce sleep-heavy tests.
