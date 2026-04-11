package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

// Output format constants. Empty string = text (default).
const (
	FormatText       = "text"
	FormatJSON       = "json"        // single final JSON object
	FormatStreamJSON = "stream-json" // JSONL events (existing --json behavior)
	FormatDiff       = "diff"        // print final diffs of edited files
)

// UsageError signals a user-input error that should become a process
// exit code but NOT bypass deferred cleanup (e.g. MCP subprocess
// teardown). Top-level command wrapper translates the typed error
// into os.Exit(ExitCode) AFTER runExec has returned and all defers
// have fired. See Codex review of v6 — os.Exit inside runExec would
// leak subprocess-backed MCP servers.
type UsageError struct {
	Msg      string
	ExitCode int
}

func (e *UsageError) Error() string { return e.Msg }

// NewUsageError returns a UsageError with exit code 64 (EX_USAGE).
func NewUsageError(format string, args ...any) *UsageError {
	return &UsageError{Msg: fmt.Sprintf(format, args...), ExitCode: 64}
}

// Params configures a headless execution run.
type Params struct {
	// --- existing fields (preserved for backward compat) ---
	EngineParams engine.EngineParams
	Engine       *engine.Engine // if set, use this engine (skips New)
	Prompt       string
	JSON         bool      // emit JSONL events to Writer (alias for OutputFormat=stream-json)
	Quiet        bool      // suppress banner
	Model        string    // for banner display
	Auth         string    // credential source for banner
	Writer       io.Writer // defaults to os.Stdout

	// --- Phase 1: output format + observability ---
	// OutputFormat picks the stdout shape. Empty = text.
	OutputFormat string

	// Verbose emits tool args + thinking blocks in text mode.
	Verbose bool

	// PrintCost forces a cost summary to stderr at end of run,
	// independent of TUI detection.
	PrintCost bool

	// PrintTools logs each tool call as [name] to stderr,
	// independent of TUI detection.
	PrintTools bool

	// PrintTree prints an end-of-run ASCII tool tree to stderr.
	// Flat rendering only — subagent tools prefixed with [role],
	// true nesting deferred (needs parent_id on event.ToolCall).
	PrintTree bool

	// ShowSystem prints the assembled system prompt to stderr at
	// start (debugging aid).
	ShowSystem bool

	// Artifact outputs — each is a filesystem path. Empty = disabled.
	SaveTranscript string // full JSONL transcript of the run
	SaveCost       string // JSON cost report
	SaveDiff       string // final unified diff of edited files
}

// Validate enforces mutual-exclusion rules that Cobra can't express
// directly (value-dependent flag combinations). Called from Run
// before any engine work.
func (p *Params) Validate() error {
	switch p.OutputFormat {
	case "", FormatText, FormatJSON, FormatStreamJSON, FormatDiff:
		// ok
	default:
		return NewUsageError(
			"invalid --output-format %q (want text|json|stream-json|diff)",
			p.OutputFormat,
		)
	}
	// --quiet + --verbose are contradictory
	if p.Quiet && p.Verbose {
		return NewUsageError("--quiet and --verbose are mutually exclusive")
	}
	// --quiet + --show-system suppresses what we'd show
	if p.Quiet && p.ShowSystem {
		return NewUsageError("--quiet and --show-system are mutually exclusive")
	}
	// --output-format diff + --verbose — nothing to be verbose about
	if p.OutputFormat == FormatDiff && p.Verbose {
		return NewUsageError("--output-format diff and --verbose are mutually exclusive")
	}
	// Cross-check: --json and --output-format conflict only if they disagree.
	// --json alone is kept as a backward-compat alias for stream-json, so
	// `--json` + `--output-format stream-json` is fine.
	if p.JSON && p.OutputFormat != "" && p.OutputFormat != FormatStreamJSON {
		return NewUsageError(
			"--json conflicts with --output-format %s (use one)", p.OutputFormat)
	}
	return nil
}

// effectiveFormat returns the canonical format string after normalizing
// the --json alias.
func (p *Params) effectiveFormat() string {
	if p.OutputFormat != "" {
		return p.OutputFormat
	}
	if p.JSON {
		return FormatStreamJSON
	}
	return FormatText
}

// Run executes a single prompt headlessly and writes output.
func Run(ctx context.Context, p Params) error {
	if err := p.Validate(); err != nil {
		return err
	}

	eng := p.Engine
	if eng == nil {
		var err error
		eng, err = engine.New(p.EngineParams)
		if err != nil {
			return fmt.Errorf("create engine: %w", err)
		}
	}

	w := p.Writer
	if w == nil {
		w = os.Stdout
	}

	// --show-system prints the assembled system prompt to stderr
	// before the run starts. Uses the engine's discovered instructions
	// (CLAUDE.md cascade, project instructions, etc.). Can't access
	// the fully-composed provider-level system prompt yet — that lives
	// inside the engine's Run loop. For now, dump instructions.
	if p.ShowSystem {
		for _, inst := range eng.Instructions() {
			fmt.Fprintf(os.Stderr, "=== %s ===\n%s\n\n", inst.Path, inst.Content)
		}
	}

	format := p.effectiveFormat()

	// Banner appears only for text mode in an interactive terminal.
	// JSONL modes pipe machine-readable output; adding ANSI chrome
	// would corrupt the stream.
	showBanner := format == FormatText && !p.Quiet && isTerminal(w)
	if showBanner {
		printBanner(w, p)
	}

	start := time.Now()
	ch := eng.Run(ctx, p.Prompt)

	var err error
	switch format {
	case FormatStreamJSON:
		err = drainJSON(ctx, ch, w, &p)
	case FormatJSON:
		err = drainJSONFinal(ctx, ch, w, &p)
	case FormatDiff:
		err = drainDiff(ctx, ch, w, &p)
	default: // FormatText
		err = drainText(ctx, ch, w, &p)
	}

	// Footer (cost + timing) shows when:
	//   * we're in text mode inside an interactive terminal, OR
	//   * the user explicitly asked for --print-cost regardless of TTY.
	// The second case lets scripts capture cost without parsing JSONL.
	if showBanner {
		printFooter(w, time.Since(start), eng)
	} else if p.PrintCost {
		printCostToStderr(time.Since(start), eng)
	}
	return err
}

// drainJSONFinal collects the full turn into a single JSON object
// emitted at Done. Used by --output-format json. Unlike stream-json
// (which is a JSONL of raw events), this emits one structured result
// with text, tool calls, cost, and any errors/permissions.
//
// Fidelity notes:
//   - ToolStart carries a partial input; the real input is reconciled
//     on ToolResultEvent (see engine.go:853 where the engine re-sends
//     the original Input). We update rec.Input on result so the final
//     JSON reflects what the tool actually saw.
//   - Multiple ErrorEvents are accumulated, not clobbered — a reviewer
//     pointed out the previous single-slot version lost earlier errors.
//   - PermissionRequest auto-denies are recorded in result.Permissions
//     so scripts can detect "work didn't happen because a tool was
//     denied" without guessing from the text output. Phase 3 replaces
//     the auto-deny path with real --permission-prompt-tool routing.
func drainJSONFinal(ctx context.Context, ch <-chan event.Event, w io.Writer, p *Params) error {
	type toolCallRecord struct {
		Name   string          `json:"name"`
		Input  json.RawMessage `json:"input,omitempty"`
		Output string          `json:"output,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	type permissionRecord struct {
		Tool    string `json:"tool"`
		Pattern string `json:"pattern"`
		Action  string `json:"action"` // "auto-deny" in Phase 1
	}
	type finalResult struct {
		Text        string             `json:"text"`
		Thinking    string             `json:"thinking,omitempty"`
		Tools       []toolCallRecord   `json:"tools,omitempty"`
		Permissions []permissionRecord `json:"permissions,omitempty"`
		Usage       *event.UsageInfo   `json:"usage,omitempty"`
		Errors      []string           `json:"errors,omitempty"`
	}

	var result finalResult
	toolsByID := map[string]*toolCallRecord{}
	var toolOrder []string

	for ev := range ch {
		switch ev.Type {
		case event.TextDelta:
			result.Text += ev.Text
		case event.ThinkingDelta:
			result.Thinking += ev.Thinking
		case event.ToolStart:
			if ev.ToolCall == nil {
				continue
			}
			rec := &toolCallRecord{
				Name:  ev.ToolCall.Name,
				Input: ev.ToolCall.Input,
			}
			toolsByID[ev.ToolCall.ID] = rec
			toolOrder = append(toolOrder, ev.ToolCall.ID)
		case event.ToolResultEvent:
			if ev.ToolResult == nil || ev.ToolCall == nil {
				continue
			}
			rec, ok := toolsByID[ev.ToolCall.ID]
			if !ok {
				// Should not happen, but if ToolResult arrives without
				// a preceding ToolStart (provider quirks), still record.
				rec = &toolCallRecord{Name: ev.ToolCall.Name}
				toolsByID[ev.ToolCall.ID] = rec
				toolOrder = append(toolOrder, ev.ToolCall.ID)
			}
			// Engine re-sends the full Input on ToolResult; prefer
			// that over ToolStart's partial payload.
			if len(ev.ToolCall.Input) > 0 {
				rec.Input = ev.ToolCall.Input
			}
			rec.Output = ev.ToolResult.Output
			rec.Error = ev.ToolResult.Error
		case event.UsageEvent:
			if ev.Usage != nil {
				// Accumulate — some providers emit multiple usage
				// events per turn. Pinned to event.UsageInfo shape at
				// internal/event/event.go:51; if that struct gains a
				// field, add it here.
				if result.Usage == nil {
					result.Usage = &event.UsageInfo{}
				}
				result.Usage.InputTokens += ev.Usage.InputTokens
				result.Usage.OutputTokens += ev.Usage.OutputTokens
				result.Usage.CacheHits += ev.Usage.CacheHits
			}
		case event.ErrorEvent:
			if ev.Error != "" {
				result.Errors = append(result.Errors, ev.Error)
			}
		case event.PermissionRequest:
			// Phase 3 will wire --permission-prompt-tool. In Phase 1
			// we have no responder, so auto-deny to unblock the engine
			// AND record the denial so callers can see what was blocked.
			if ev.Permission != nil {
				result.Permissions = append(result.Permissions, permissionRecord{
					Tool:    ev.Permission.ToolName,
					Pattern: ev.Permission.Pattern,
					Action:  "auto-deny",
				})
				if ev.Permission.Response != nil {
					select {
					case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
					case <-ctx.Done():
					}
				}
			}
		}
	}

	// Render ordered tools from the id map.
	for _, id := range toolOrder {
		if rec := toolsByID[id]; rec != nil {
			result.Tools = append(result.Tools, *rec)
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("drain json-final encode: %w", err)
	}
	if len(result.Errors) > 0 {
		// Join all accumulated errors — previous single-slot behavior
		// lost earlier errors if more than one ErrorEvent arrived.
		return errors.New(strings.Join(result.Errors, "; "))
	}
	return nil
}

// drainDiff collects tool events, identifies edited file paths from
// edit/write/apply_patch tool calls, and prints a unified diff of
// the edited files vs HEAD at the end of the run. Phase 7 will wire
// this to the --save-diff artifact path; Phase 1 implements the
// stdout print side.
//
// Fidelity notes:
//   - apply_patch is handled by parsing the unified diff body for
//     `+++ b/<path>` lines (a reviewer caught that write/edit/"patch"
//     was the wrong tool name set — the real tool is apply_patch with
//     `{"patch": "<diff>"}`, not `{"file_path": ...}`).
//   - git diff failures used to be swallowed to stderr. Now we
//     surface them as a UsageError so scripts can detect "diff
//     requested but unavailable" (no git binary, detached HEAD, etc.).
func drainDiff(ctx context.Context, ch <-chan event.Event, w io.Writer, p *Params) error {
	editedFiles := make(map[string]struct{})
	var lastErrs []string

	for ev := range ch {
		switch ev.Type {
		case event.ToolStart, event.ToolResultEvent:
			// Check both start and result — engine re-sends Input on
			// result (engine.go:856), and for apply_patch the full
			// patch body may only appear on the result envelope.
			if ev.ToolCall == nil {
				continue
			}
			for _, path := range extractFilePaths(ev.ToolCall.Name, ev.ToolCall.Input) {
				if path != "" {
					editedFiles[path] = struct{}{}
				}
			}
		case event.ErrorEvent:
			if ev.Error != "" {
				lastErrs = append(lastErrs, ev.Error)
			}
		case event.PermissionRequest:
			if ev.Permission != nil && ev.Permission.Response != nil {
				select {
				case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
				case <-ctx.Done():
				}
			}
		}
	}

	if len(editedFiles) == 0 {
		fmt.Fprintln(w, "(no files edited)")
	} else {
		paths := make([]string, 0, len(editedFiles))
		for f := range editedFiles {
			paths = append(paths, f)
		}
		args := append([]string{"diff", "--no-color", "HEAD", "--"}, paths...)
		cmd := osexec.CommandContext(ctx, "git", args...)
		cmd.Stdout = w
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// Surface the failure: scripts need to tell "diff
			// produced nothing" (acceptable) apart from "git failed"
			// (actionable). Return a UsageError so the top-level
			// command translates it to an exit code.
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return NewUsageError("git diff failed: %s", msg)
		}
	}
	if len(lastErrs) > 0 {
		return errors.New(strings.Join(lastErrs, "; "))
	}
	return nil
}

// extractFilePaths pulls file paths out of a tool call's input JSON
// for tools known to edit files. Used by drainDiff to figure out
// which files to include in the final diff. Unknown tools return nil.
//
// Returns a slice because apply_patch can touch multiple files in
// a single call; the other file tools return at most one.
func extractFilePaths(toolName string, input json.RawMessage) []string {
	switch toolName {
	case "write", "edit":
		var p struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(input, &p); err == nil && p.FilePath != "" {
			return []string{p.FilePath}
		}
	case "apply_patch":
		var p struct {
			Patch string `json:"patch"`
		}
		if err := json.Unmarshal(input, &p); err == nil {
			return parsePatchPaths(p.Patch)
		}
	}
	return nil
}

// parsePatchPaths extracts the `b/<path>` side of each file header
// in a unified diff. `+++ b/foo.go` → "foo.go". Returns all paths
// in the patch (apply_patch can touch many files in one call).
func parsePatchPaths(patch string) []string {
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		if !strings.HasPrefix(line, "+++ ") {
			continue
		}
		rest := strings.TrimSpace(line[4:])
		// Strip `b/` prefix if present (standard unified diff),
		// otherwise use the raw path (custom apply_patch forms).
		rest = strings.TrimPrefix(rest, "b/")
		// Drop timestamp suffix if any (`+++ b/foo.go\t2024-...`)
		if tab := strings.IndexByte(rest, '\t'); tab >= 0 {
			rest = rest[:tab]
		}
		if rest != "" && rest != "/dev/null" {
			paths = append(paths, rest)
		}
	}
	return paths
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

const (
	dim    = "\033[2m"
	bold   = "\033[1m"
	green  = "\033[32m"
	purple = "\033[35m"
	cyan   = "\033[36m"
	reset  = "\033[0m"
)

func printBanner(w io.Writer, p Params) {
	model := p.Model
	if model == "" {
		model = "auto"
	}
	fmt.Fprintf(w, "%s╭─ %saltcode%s %s%s%s", dim, bold+purple, reset, dim, model, reset)
	if p.Auth != "" {
		fmt.Fprintf(w, " %s(%s)%s", dim, p.Auth, reset)
	}
	fmt.Fprintf(w, "\n%s│%s\n", dim, reset)
}

func printFooter(w io.Writer, elapsed time.Duration, eng *engine.Engine) {
	ms := elapsed.Milliseconds()
	var timing string
	if ms < 1000 {
		timing = fmt.Sprintf("%dms", ms)
	} else {
		timing = fmt.Sprintf("%.1fs", elapsed.Seconds())
	}
	cost := ""
	if eng != nil {
		if ct := eng.CostTracker(); ct != nil {
			in, out := ct.TotalTokens()
			total := ct.TotalCost()
			if in+out > 0 {
				if total > 0 {
					cost = fmt.Sprintf(" %s· %d in / %d out · $%.4f%s", dim, in, out, total, reset)
				} else {
					cost = fmt.Sprintf(" %s· %d in / %d out%s", dim, in, out, reset)
				}
			}
		}
	}
	fmt.Fprintf(w, "%s│%s\n", dim, reset)
	fmt.Fprintf(w, "%s╰─ %s%s%s%s\n", dim, green, timing, reset, cost)
}

func truncatePrompt(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// drainText is the default (human-readable) drain. `p` is always
// non-nil — Run() passes &p. Kept the param as a pointer (not value)
// to match drainJSON/drainJSONFinal/drainDiff and allow later phases
// to mutate Params during the run if needed.
func drainText(ctx context.Context, ch <-chan event.Event, w io.Writer, p *Params) error {
	var lastErr string
	// showTools controls whether tool-call chatter goes to stderr.
	// Default: terminal detection (old behavior). Explicit flag
	// overrides so scripts can opt in or out.
	showTools := isTerminal(w)
	if p.PrintTools {
		showTools = true
	}
	if p.Quiet {
		showTools = false
	}
	for ev := range ch {
		switch ev.Type {
		case event.TextDelta:
			if !p.Quiet {
				fmt.Fprint(w, ev.Text)
			}
		case event.ThinkingDelta:
			// Thinking blocks only surface under --verbose; otherwise
			// they're noise. Route to stderr so they don't corrupt
			// piped stdout.
			if p.Verbose && ev.Thinking != "" {
				fmt.Fprintf(os.Stderr, "%s[thinking] %s%s\n", dim, ev.Thinking, reset)
			}
		case event.ToolStart:
			if showTools && ev.ToolCall != nil {
				if p.Verbose {
					// Verbose: include the input payload so users can
					// see exactly what the tool was asked to do.
					fmt.Fprintf(os.Stderr, "%s[%s] %s%s ",
						dim, ev.ToolCall.Name,
						truncatePrompt(string(ev.ToolCall.Input), 120),
						reset)
				} else {
					fmt.Fprintf(os.Stderr, "%s[%s]%s ", dim, ev.ToolCall.Name, reset)
				}
			}
		case event.ToolResultEvent:
			if showTools && ev.ToolResult != nil {
				if ev.ToolResult.Error != "" {
					fmt.Fprintf(os.Stderr, "%s✗%s\n", dim, reset)
				} else {
					fmt.Fprintf(os.Stderr, "%s✓%s\n", dim, reset)
				}
			}
		case event.InfoEvent:
			if ev.Info != "" && showTools {
				fmt.Fprintf(os.Stderr, "%s%s%s\n", dim, ev.Info, reset)
			}
		case event.PermissionRequest:
			// Phase 1 has no --permission-prompt-tool wiring yet.
			// Auto-deny so the engine doesn't block forever. Phase 3
			// replaces this with a proper MCP-routed responder.
			if ev.Permission != nil && ev.Permission.Response != nil {
				select {
				case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
				case <-ctx.Done():
				}
				fmt.Fprintf(os.Stderr,
					"%saltcode: permission request for %s auto-denied "+
						"(Phase 3 will add --permission-prompt-tool)%s\n",
					dim, ev.Permission.ToolName, reset)
			}
		case event.ErrorEvent:
			lastErr = ev.Error
		case event.Done:
			if !p.Quiet {
				fmt.Fprintln(w)
			}
		}
	}
	if lastErr != "" {
		return errors.New(lastErr)
	}
	return nil
}

func drainJSON(ctx context.Context, ch <-chan event.Event, w io.Writer, p *Params) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	var lastErr string
	var encodeErr error
	for ev := range ch {
		// BLOCKER FIX: always answer permission requests, even after
		// an encode error. Previously the auto-deny was nested inside
		// `if encodeErr == nil`, so an EPIPE on stdout followed by a
		// permission-gated tool call would consume the event without
		// sending the deny response — engine.askPermission blocks on
		// <-respCh forever. The answer path is independent from the
		// stdout path; keep them decoupled.
		if ev.Type == event.PermissionRequest && ev.Permission != nil && ev.Permission.Response != nil {
			select {
			case ev.Permission.Response <- event.PermResponse{Action: event.Deny}:
			case <-ctx.Done():
			}
		}

		// On encode error (broken pipe, disk full, etc.) stop writing
		// but KEEP DRAINING the channel. The engine goroutine is still
		// sending events; if we return early, its next send blocks
		// forever and leaks the engine + provider HTTP conn + tool
		// subprocesses. Caller's ctx cancel can't unblock a stuck send.
		if encodeErr == nil {
			// event.PermReq.Response is already tagged `json:"-"`
			// (event.go:67), so enc.Encode(ev) omits the channel
			// field without manual sanitization.
			if err := enc.Encode(ev); err != nil {
				encodeErr = err
			}
		}
		if ev.Type == event.ErrorEvent {
			lastErr = ev.Error
		}
	}
	if encodeErr != nil {
		return fmt.Errorf("drain json encode: %w", encodeErr)
	}
	if lastErr != "" {
		return errors.New(lastErr)
	}
	return nil
}

// printCostToStderr writes the cost + timing footer to stderr only.
// Used when --print-cost is set without a TUI banner (e.g., piping
// output through a tool that still wants cost info out-of-band).
func printCostToStderr(elapsed time.Duration, eng *engine.Engine) {
	ms := elapsed.Milliseconds()
	var timing string
	if ms < 1000 {
		timing = fmt.Sprintf("%dms", ms)
	} else {
		timing = fmt.Sprintf("%.1fs", elapsed.Seconds())
	}
	if eng == nil {
		fmt.Fprintf(os.Stderr, "altcode: %s\n", timing)
		return
	}
	ct := eng.CostTracker()
	if ct == nil {
		fmt.Fprintf(os.Stderr, "altcode: %s\n", timing)
		return
	}
	in, out := ct.TotalTokens()
	total := ct.TotalCost()
	if total > 0 {
		fmt.Fprintf(os.Stderr, "altcode: %s · %d in / %d out · $%.4f\n",
			timing, in, out, total)
	} else {
		fmt.Fprintf(os.Stderr, "altcode: %s · %d in / %d out\n", timing, in, out)
	}
}
