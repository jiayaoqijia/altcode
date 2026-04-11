package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const defaultTimeout = 30

func runCommandHook(ctx context.Context, entry EntryConfig, input Input) (*Result, error) {
	timeout := entry.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", entry.Command)
	cmd.WaitDelay = time.Second // ensure child processes are cleaned up

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
			Decision: "allow",
			Message:  stdout.String(),
		}, nil
	}
	return &result, nil
}
