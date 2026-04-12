package exec

// Phase 3: --permission-prompt-tool implementation.
//
// When altcode runs headless with `--permission-mode default` the
// engine emits `event.PermissionRequest` for any tool call that
// doesn't match an allow/deny rule and waits on the `Response`
// channel for an allow/deny decision. With no TUI to answer,
// the engine blocks forever — or, with the Phase 1 stopgap,
// auto-denies every request and nothing ever runs.
//
// Phase 3 closes the gap by letting the user point altcode at an
// MCP tool that answers permission prompts. The drain wrapper
// spawns a goroutine per request, invokes the tool via the shared
// `tool.Registry`, parses the JSON body for an allow/deny decision,
// and feeds the result back into the engine's response channel.
//
// This matches Claude Code's `--permission-prompt-tool` semantics
// (yes/no response), though we don't yet support `updatedInput`
// rewrites — that's future work once the permission evaluator
// gains a `Rewrite` field.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/tool"
)

// promptToolResponse is the JSON shape expected from a
// --permission-prompt-tool call. Minimal schema: {"allow": bool,
// "reason": "..."}. Any parse error is treated as a deny with a
// diagnostic.
type promptToolResponse struct {
	Allow  bool   `json:"allow"`
	Reason string `json:"reason,omitempty"`
}

// promptToolTimeout bounds each prompt-tool invocation. An
// unresponsive tool should never wedge the entire run.
const promptToolTimeout = 30 * time.Second

// handlePermissionRequest answers a single PermissionRequest event
// by invoking the configured MCP prompt tool. When no tool is
// configured, it fails closed (deny) with a clear diagnostic so
// headless mode doesn't silently wedge.
//
// Safe for concurrent use — spawns a goroutine per call so the
// drain keeps reading events while a slow prompt tool runs.
func handlePermissionRequest(
	ctx context.Context,
	ev event.Event,
	promptTool string,
	registry *tool.Registry,
) {
	if ev.Permission == nil || ev.Permission.Response == nil {
		return
	}

	if promptTool == "" {
		// Phase 1 fallback: auto-deny with a clear message so
		// users understand why their tool call was blocked.
		select {
		case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
		case <-ctx.Done():
		}
		fmt.Fprintf(os.Stderr,
			"altcode: permission request for %s denied "+
				"(headless mode requires --permission-prompt-tool)\n",
			ev.Permission.ToolName)
		return
	}

	if registry == nil {
		// Engine didn't hand us a registry — treat as a
		// configuration error but still answer the request so
		// the engine doesn't deadlock.
		select {
		case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
		case <-ctx.Done():
		}
		fmt.Fprintln(os.Stderr,
			"altcode: --permission-prompt-tool set but no tool registry available; denying")
		return
	}

	t, ok := registry.Get(promptTool)
	if !ok {
		// Misconfigured tool name. Deny + diagnostic.
		select {
		case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
		case <-ctx.Done():
		}
		fmt.Fprintf(os.Stderr,
			"altcode: --permission-prompt-tool %q not found in registry; "+
				"check MCP server config or run `altcode --print-mcp`\n",
			promptTool)
		return
	}

	// Spawn the tool call in its own goroutine so an unresponsive
	// prompt tool doesn't wedge the drain. The goroutine owns the
	// response channel send — we return immediately.
	go func() {
		callCtx, cancel := context.WithTimeout(ctx, promptToolTimeout)
		defer cancel()

		payload, _ := json.Marshal(map[string]any{
			"tool_name": ev.Permission.ToolName,
			"pattern":   ev.Permission.Pattern,
		})
		// tool.Tool.Execute wants json.RawMessage (not []byte);
		// explicit cast so the call site is unambiguous.
		result, err := t.Execute(callCtx, json.RawMessage(payload))
		action := event.Deny
		reason := ""
		if err != nil {
			reason = err.Error()
		} else if result == nil || result.Error != nil {
			if result != nil && result.Error != nil {
				reason = result.Error.Error()
			}
		} else {
			var resp promptToolResponse
			if jerr := json.Unmarshal([]byte(result.Output), &resp); jerr != nil {
				reason = fmt.Sprintf("parse response: %v", jerr)
			} else {
				reason = resp.Reason
				if resp.Allow {
					action = event.Allow
				}
			}
		}

		// Send the decision back to the engine. Non-blocking on
		// ctx.Done() so a late cancellation doesn't wedge the
		// goroutine.
		select {
		case ev.Permission.Response <- event.PermResponse{Action: action}:
		case <-ctx.Done():
			return
		}

		// Diagnostic: always print a one-line summary so users
		// can audit what the prompt tool decided.
		verdict := "allow"
		if action != event.Allow {
			verdict = "deny"
		}
		if reason != "" {
			fmt.Fprintf(os.Stderr,
				"altcode: --permission-prompt-tool %s for %s → %s (%s)\n",
				promptTool, ev.Permission.ToolName, verdict, reason)
		} else {
			fmt.Fprintf(os.Stderr,
				"altcode: --permission-prompt-tool %s for %s → %s\n",
				promptTool, ev.Permission.ToolName, verdict)
		}
	}()
}

// validatePromptToolName is called from Params.Validate() to catch
// bareword tool names (e.g. `--permission-prompt-tool approve`)
// before the run starts. MCP tools are always prefixed
// `mcp__<server>__<tool>` in the registry (see
// internal/mcp/tools.go RegisterMCPTools).
func validatePromptToolName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if !strings.HasPrefix(name, "mcp__") {
		return fmt.Errorf(
			"--permission-prompt-tool %q must start with 'mcp__<server>__' prefix "+
				"— MCP tools are registered under their server name",
			name)
	}
	return nil
}
