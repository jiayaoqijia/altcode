package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const defaultTimeout = 30

// maxHookDepth caps how deeply hooks can recursively invoke altcode
// (or any tool that re-runs hooks). Phase 6 added this guard
// because a hook shelling out to `altcode` would otherwise fork-bomb
// the system when its child re-registered the same hook.
const maxHookDepth = 3

// HookDepthExceeded is returned (as the error in the Result) when
// ALTCODE_HOOK_DEPTH is already at the cap and firing another hook
// would exceed it. Converted to a "deny" Result for safety — failing
// open would cancel the recursion guard's purpose.
var HookDepthExceeded = fmt.Errorf("hook depth exceeded (max %d)", maxHookDepth)

// currentHookDepth reads the ALTCODE_HOOK_DEPTH env var set by
// parent altcode invocations. Returns 0 when unset or non-numeric.
func currentHookDepth() int {
	s := os.Getenv("ALTCODE_HOOK_DEPTH")
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// DepthGuardTripped returns true if the current process's hook
// depth is at or past the cap. Used by cmd/altcode/main.go at
// startup to refuse to register any hooks on a recursive invocation.
func DepthGuardTripped() bool {
	return currentHookDepth() >= maxHookDepth
}

func runCommandHook(ctx context.Context, entry EntryConfig, input Input) (*Result, error) {
	// Phase 6: recursion guard. A hook that shells out to `altcode`
	// would otherwise fork-bomb the system because the child
	// invocation re-registers the same hook. We propagate
	// ALTCODE_HOOK_DEPTH via the child env; the child reads it
	// at startup and refuses to register hooks when depth > cap.
	//
	// Here we still ALLOW the run (so the top-level hook fires
	// once), but we increment the depth for the child shell.
	depth := currentHookDepth()
	if depth >= maxHookDepth {
		return &Result{
			Decision: "deny",
			Message: fmt.Sprintf(
				"altcode: refusing to run command hook — "+
					"ALTCODE_HOOK_DEPTH=%d already at cap %d",
				depth, maxHookDepth),
		}, nil
	}

	timeout := entry.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", entry.Command)
	cmd.WaitDelay = time.Second // ensure child processes are cleaned up
	// Group the hook child + its descendants so we can SIGKILL them all
	// on timeout. Without this, a hook like `sh -c "validator | helper"`
	// would leave `helper` orphaned when the timeout fires.
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }

	// Inject ALTCODE_HOOK_DEPTH into the child env so nested
	// altcode invocations can detect recursion and refuse.
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("ALTCODE_HOOK_DEPTH=%d", depth+1))

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal hook input: %w", err)
	}
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Exit code 2 = block (stderr is the message)
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		return &Result{
			Decision: "deny",
			Message:  stderr.String(),
		}, nil
	}

	// Timeout = fail CLOSED (deny). For a security control, an
	// unresponsive hook should never silently allow the action.
	// Previously this turned into 'allow', which let dangerous
	// commands slip through if a slow hook validator stalled.
	if ctx.Err() == context.DeadlineExceeded {
		return &Result{
			Decision: "deny",
			Message:  fmt.Sprintf("hook timed out after %ds; failing closed", timeout),
		}, nil
	}

	// Other errors = hook itself failed (process spawn, etc).
	// Surface the error to the caller so it can decide policy.
	if err != nil {
		return nil, fmt.Errorf("hook command failed: %w: %s", err, stderr.String())
	}

	// Parse JSON result from stdout
	if stdout.Len() == 0 {
		return &Result{Decision: "allow"}, nil
	}

	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return &Result{
			Decision: "",
			Message:  stdout.String(),
		}, nil
	}
	return &result, nil
}
