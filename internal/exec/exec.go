package exec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/hooks"
	"github.com/altcode-ai/altcode/internal/permission"
	"github.com/altcode-ai/altcode/internal/provider"
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

	// --- Phase 2: permission / mode ---
	// PermissionMode picks the evaluator mode. Empty = leave at
	// the evaluator's existing mode (default). Valid values:
	// "plan" | "auto" | "default" | "bypass". Named
	// --permission-mode (not --mode) to avoid collision with the
	// existing `workflow --mode` subcommand flag.
	PermissionMode string

	// AllowTools is a list of "name[:pattern]" session-allow rules
	// added on top of the config rules. A pattern-less entry
	// matches any pattern (pattern "*"). Repeatable CLI flag.
	AllowTools []string

	// DenyTools mirrors AllowTools but adds deny rules. Deny
	// takes precedence over allow (see permission.Check at
	// permission.go:84-92).
	DenyTools []string

	// DryRun is Phase 2's simplest implementation: alias for
	// PermissionMode="plan". Spec v7 gave dry-run its own UX
	// (agent keeps running, writes logged as [DRY-RUN]), but
	// that needs engine-level tool-execution interception.
	// Phase 2 takes the plan-mode shortcut; a future phase can
	// diverge if users ask for it.
	DryRun bool

	// --- Phase 8: budgets ---
	// MaxTurns overrides the engine's default agent-loop iteration
	// cap (maxIterations = 50). 0 = use default. Wired to
	// EngineParams.MaxTurns in main.go's runExec.
	MaxTurns int

	// MaxCost is a post-turn USD budget. When the accumulated
	// session cost exceeds this value, the engine emits
	// BudgetExceeded and returns before the next provider call.
	// 0 = unlimited. Propagated to subagents via
	// engine.CostBudget.
	MaxCost float64

	// --- Phase 5: input flags ---
	// Images is a list of filesystem paths (or "-" for stdin) to
	// attach as image content blocks on the first user message.
	// Supports Anthropic multimodal shape; OpenAI multimodal is
	// future work. Phase 5 ships Anthropic-only.
	Images []string

	// Files is a list of filesystem paths whose contents are
	// injected into the prompt as pre-loaded context. Each file
	// is wrapped as a fenced code block with the path as a tag.
	// Unlike --image, this is a text-only mechanism that works
	// across all providers.
	Files []string

	// PromptFile replaces the Prompt text with contents of a file.
	// "-" means stdin. When both Prompt and PromptFile are set,
	// Validate() errors.
	PromptFile string

	// System appends a free-form string to the assembled system
	// prompt. Useful for one-off overrides without editing config.
	System string

	// SystemFile is a path whose contents are appended to the
	// system prompt. Combines with --system (both are appended).
	SystemFile string

	// --- Phase 7: artifacts + commit ---
	// Commit triggers a `git commit` at the end of a successful
	// run with an auto-generated commit message. Refuses if:
	//   - --permission-mode plan was used (nothing was written)
	//   - --dry-run was used (nothing was executed)
	//   - working tree was dirty BEFORE the run (unless CommitDirty)
	//   - the run produced no changes (silent success, exit 0)
	Commit bool

	// CommitDirty bypasses the clean-working-tree guard. Mixes
	// human + agent changes in the same commit; loud stderr
	// warning at startup.
	CommitDirty bool

	// preRunDirty captures `git status --porcelain` output at
	// the entry to Run(). Used by the commit path to detect
	// whether the working tree was dirty before the run started.
	// Populated by Run(), not the caller.
	preRunDirty string

	// --- Phase 6: hooks + extension ---
	// Hooks is a list of "<event>:<shell-command>" strings.
	// Each entry is parsed into a hooks.MatcherConfig and
	// registered on the engine's hook runner before Run() starts.
	// Repeatable CLI flag. `:` is the separator (not `=`) so
	// commands containing `=` aren't ambiguous.
	Hooks []string

	// MCPServers is a list of "<name>:<shell-command>" strings
	// that register ad-hoc MCP servers for this run. Merged with
	// any cfg.MCP entries instead of replacing them.
	MCPServers []string

	// Skills is a list of skill names to preload into the
	// system prompt for this run. Match against discovered
	// skills by name; unknown names fail at validation time.
	Skills []string
}

// Permission mode constants. Match the permission.Mode enum at
// internal/permission/permission.go:8-13 but kept as strings for
// CLI parsing. Empty string = leave at evaluator default.
const (
	ModePlan    = "plan"
	ModeAuto    = "auto"
	ModeDefault = "default"
	ModeBypass  = "bypass"
)

// validPermissionModes is the set of accepted --permission-mode values.
var validPermissionModes = map[string]bool{
	ModePlan:    true,
	ModeAuto:    true,
	ModeDefault: true,
	ModeBypass:  true,
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
	// --permission-mode value check (value-dependent, can't use Cobra).
	if p.PermissionMode != "" && !validPermissionModes[p.PermissionMode] {
		return NewUsageError(
			"invalid --permission-mode %q (want plan|auto|default|bypass)",
			p.PermissionMode,
		)
	}
	// --permission-mode bypass + explicit denies is contradictory —
	// bypass allows everything. Allow entries are still fine (no-op).
	if p.PermissionMode == ModeBypass && len(p.DenyTools) > 0 {
		return NewUsageError(
			"--permission-mode bypass cannot be combined with --deny-tool " +
				"(bypass allows everything)")
	}
	// Phase 8: budget value checks. 0 means "use engine default"
	// so only reject strictly-negative values. Codex Phase 8
	// review caught the docstring mismatch.
	if p.MaxTurns < 0 {
		return NewUsageError("--max-turns must be >= 0 (got %d; 0 = engine default)", p.MaxTurns)
	}
	if p.MaxCost < 0 {
		return NewUsageError("--max-cost must be >= 0 (got %.4f; 0 = unlimited)", p.MaxCost)
	}
	// Phase 7: --commit is mutually exclusive with anything that
	// produces no changes.
	if p.Commit {
		if p.DryRun {
			return NewUsageError(
				"--commit and --dry-run are mutually exclusive " +
					"(dry-run doesn't write, nothing to commit)")
		}
		if p.PermissionMode == ModePlan {
			return NewUsageError(
				"--commit and --permission-mode plan are mutually exclusive " +
					"(plan mode denies writes, nothing to commit)")
		}
	}

	// Phase 6: --hook format check. Each entry must be
	// "<event>:<command>" with a known event name. Command
	// validation is not attempted here — empty commands would
	// no-op at runtime which is harmless.
	for _, h := range p.Hooks {
		if _, _, err := parseHookSpec(h); err != nil {
			return NewUsageError("invalid --hook %q: %v", h, err)
		}
	}
	// --mcp format check.
	for _, m := range p.MCPServers {
		if _, _, err := parseMCPSpec(m); err != nil {
			return NewUsageError("invalid --mcp %q: %v", m, err)
		}
	}
	// --allow-tool / --deny-tool format check: each entry must be
	// "name" or "name:pattern". Empty name is an error.
	for _, s := range p.AllowTools {
		if _, _, err := parseToolRuleSpec(s); err != nil {
			return NewUsageError("invalid --allow-tool %q: %v", s, err)
		}
	}
	for _, s := range p.DenyTools {
		if _, _, err := parseToolRuleSpec(s); err != nil {
			return NewUsageError("invalid --deny-tool %q: %v", s, err)
		}
	}
	// Phase 5: stdin consumer checks. Only one source can read
	// from FD 0 because binary image bytes + text prompt bytes
	// can't multiplex on the same stream.
	stdinConsumers := 0
	if p.PromptFile == "-" {
		stdinConsumers++
	}
	for _, img := range p.Images {
		if img == "-" {
			stdinConsumers++
		}
	}
	if stdinConsumers > 1 {
		return NewUsageError(
			"at most one of --prompt-file=- / --image=- may read stdin "+
				"(%d consumers requested)",
			stdinConsumers,
		)
	}
	// --prompt-file and a positional prompt are redundant and
	// likely indicate user confusion. Error with a clear message
	// instead of silently preferring one over the other.
	if p.PromptFile != "" && p.Prompt != "" {
		return NewUsageError(
			"--prompt-file and positional prompt are mutually exclusive "+
				"(got both: file=%q, prompt=%q)",
			p.PromptFile, truncatePrompt(p.Prompt, 40),
		)
	}
	return nil
}

// Max bytes for a single --file injection. Keeps a gigantic log
// file from blowing the context window. Phase 5 review caught this
// as a missing guard — 200 MB log files would otherwise be silently
// serialized into the prompt.
const maxFileContextBytes = 1 * 1024 * 1024 // 1 MB

// longestBacktickFence returns a run of backticks one longer than
// the longest backtick sequence found in content. Used to wrap
// --file output so a file containing ``` can't terminate the
// wrapper early. Minimum fence is 3 backticks for readability.
func longestBacktickFence(content string) string {
	longest := 0
	run := 0
	for _, c := range content {
		if c == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

// PrepareInputs applies Phase 5 input flags to the Params and
// EngineParams:
//   - Reads --prompt-file into p.Prompt (or stdin for "-")
//   - Reads --system-file and appends to Instructions
//   - Appends --system text to Instructions
//   - Reads --file paths, wraps each as a fenced code block, and
//     prepends to the prompt text so the agent sees the context
//     without having to call read().
//   - Reads --image paths, base64-encodes, and fills
//     EngineParams.PendingInputParts so the engine merges them
//     into the first user message.
//
// Must be called BEFORE exec.Run constructs the engine. Returns a
// UsageError for user-input problems (missing file, unsupported
// image type, etc.).
func (p *Params) PrepareInputs(stdin io.Reader) error {
	// Provider gate for --image: Phase 5 ships Anthropic multimodal
	// only. If the user passes --image with an OpenAI / Chinese
	// provider / etc., fail fast with a clear message instead of
	// letting the ContentPart be silently dropped by toOpenAIMessages.
	//
	// Strictness: only an explicit "anthropic/..." prefix passes.
	// Bareword models like `--model gpt-4o` or an empty model
	// string are REJECTED because defaulting to anthropic for those
	// would hit a downstream API mismatch anyway. A user who
	// wants multimodal on Anthropic must use the canonical prefix.
	if len(p.Images) > 0 {
		var modelStr string
		if p.EngineParams.Config != nil {
			modelStr = p.EngineParams.Config.Model
		}
		if !strings.HasPrefix(modelStr, "anthropic/") {
			return NewUsageError(
				"--image requires an anthropic/ model prefix (got %q); "+
					"multimodal for other providers is future work",
				modelStr)
		}
	}

	// 1. --prompt-file
	if p.PromptFile != "" {
		var data []byte
		var err error
		if p.PromptFile == "-" {
			if stdin == nil {
				stdin = os.Stdin
			}
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(p.PromptFile)
		}
		if err != nil {
			return NewUsageError("--prompt-file %q: %v", p.PromptFile, err)
		}
		// Trim CRLF too — Windows-authored files end with \r\n and
		// TrimRight(s, "\n") leaves the \r dangling.
		p.Prompt = strings.TrimRight(string(data), "\r\n")
	}

	// 2. --file entries become pre-loaded context in the prompt text
	if len(p.Files) > 0 {
		var contextBlocks []string
		for _, path := range p.Files {
			info, err := os.Stat(path)
			if err != nil {
				return NewUsageError("--file %q: %v", path, err)
			}
			if info.Size() > maxFileContextBytes {
				return NewUsageError(
					"--file %q is %d bytes (max %d bytes / 1 MB) — "+
						"large files would blow the context window",
					path, info.Size(), int64(maxFileContextBytes))
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return NewUsageError("--file %q: %v", path, err)
			}
			// Use a fence longer than any run of backticks in the
			// content so an included file containing triple-backticks
			// (e.g. a markdown file or another Go file with raw
			// string literals) doesn't accidentally terminate the
			// wrapper early. Same strategy that markdown renderers use.
			content := strings.TrimRight(string(data), "\r\n")
			fence := longestBacktickFence(content)
			block := fmt.Sprintf("%s%s\n%s\n%s", fence, path, content, fence)
			contextBlocks = append(contextBlocks, block)
		}
		// Context blocks come BEFORE the user prompt text so the
		// model has the files in view when reasoning about the
		// request.
		if p.Prompt == "" {
			p.Prompt = strings.Join(contextBlocks, "\n\n")
		} else {
			p.Prompt = strings.Join(contextBlocks, "\n\n") + "\n\n" + p.Prompt
		}
	}

	// 3. --system and --system-file both append to Instructions.
	// Use DISTINCT synthetic paths ("cli:system" vs "cli:system-file")
	// so any downstream deduping by Path doesn't silently drop one.
	// The actual content from --system-file carries the source path
	// as a suffix for debuggability.
	if p.SystemFile != "" {
		data, err := os.ReadFile(p.SystemFile)
		if err != nil {
			return NewUsageError("--system-file %q: %v", p.SystemFile, err)
		}
		p.EngineParams.Instructions = append(p.EngineParams.Instructions, config.Instruction{
			Path:    "cli:system-file:" + p.SystemFile,
			Content: strings.TrimRight(string(data), "\r\n"),
		})
	}
	if p.System != "" {
		p.EngineParams.Instructions = append(p.EngineParams.Instructions, config.Instruction{
			Path:    "cli:system",
			Content: p.System,
		})
	}

	// 4. --image entries become ContentPart blocks. Anthropic-only
	// for Phase 5 (gated above). OpenAI multimodal is future work.
	if len(p.Images) > 0 {
		var parts []provider.ContentPart
		for _, path := range p.Images {
			var part provider.ContentPart
			var err error
			if path == "-" {
				if stdin == nil {
					stdin = os.Stdin
				}
				data, rerr := io.ReadAll(stdin)
				if rerr != nil {
					return NewUsageError("--image -: %v", rerr)
				}
				part, err = provider.NewImagePartFromBytes(data)
			} else {
				part, err = provider.NewImagePartFromFile(path)
			}
			if err != nil {
				return NewUsageError("--image %q: %v", path, err)
			}
			parts = append(parts, part)
		}
		p.EngineParams.PendingInputParts = append(
			p.EngineParams.PendingInputParts, parts...)

		// Image-only runs (no text prompt, no --file) get a default
		// prompt so the model has something to respond to. Anthropic
		// accepts empty text parts but models work better with a cue.
		if p.Prompt == "" {
			p.Prompt = "Describe what you see in the attached image(s)."
		}
	}
	return nil
}

// parseToolRuleSpec splits "name" or "name:pattern" into (name, pattern).
// A pattern-less entry yields pattern "*" to match any input.
// Empty names return an error.
func parseToolRuleSpec(spec string) (name, pattern string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", errors.New("empty tool rule")
	}
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		name = strings.TrimSpace(spec[:i])
		pattern = strings.TrimSpace(spec[i+1:])
		if pattern == "" {
			pattern = "*"
		}
	} else {
		name = spec
		pattern = "*"
	}
	if name == "" {
		return "", "", errors.New("empty tool name")
	}
	return name, pattern, nil
}

// ApplyCLIHooks registers each --hook entry on the runner so the
// engine sees it alongside config-defined hooks. Called from
// main.go before the engine is built.
//
// If DepthGuardTripped() returns true (we're already deep in a
// recursive hook invocation) ApplyCLIHooks returns without
// registering anything. The engine-level hook firing path also
// respects this — defense in depth.
func ApplyCLIHooks(runner *hooks.Runner, cliHooks []string) error {
	if runner == nil {
		if len(cliHooks) == 0 {
			return nil
		}
		return NewUsageError("--hook set but no hook runner available")
	}
	if hooks.DepthGuardTripped() {
		// Silently drop — the engine also refuses to fire nested
		// hooks at this depth, so registering them would be a
		// no-op anyway and the warning would spam every recursive
		// invocation.
		return nil
	}
	for _, spec := range cliHooks {
		ev, cmd, err := parseHookSpec(spec)
		if err != nil {
			return NewUsageError("invalid --hook %q: %v", spec, err)
		}
		evConst, _ := hooks.ParseEvent(ev) // validated above
		runner.AddMatcher(evConst, hooks.MatcherConfig{
			Matcher: "*", // match any tool name
			Hooks: []hooks.EntryConfig{{
				Type:    "command",
				Command: cmd,
			}},
		})
	}
	return nil
}

// ApplyCLIMCP merges --mcp entries into cfg.MCP. Called from main.go
// after config is loaded but before engine construction. Each entry
// becomes a stdio MCP server with the given command.
func ApplyCLIMCP(cfg *config.Config, cliMCP []string) error {
	if len(cliMCP) == 0 {
		return nil
	}
	if cfg.MCP == nil {
		cfg.MCP = make(map[string]config.MCPServerConfig)
	}
	for _, spec := range cliMCP {
		name, cmd, err := parseMCPSpec(spec)
		if err != nil {
			return NewUsageError("invalid --mcp %q: %v", spec, err)
		}
		// Parse the command into argv. Shell-quote-awareness is
		// intentionally minimal — users that need quoting can put
		// the logic in a wrapper script. Most MCP servers are
		// invoked like `npx -y foo-mcp` which parses cleanly on
		// whitespace.
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			return NewUsageError("invalid --mcp %q: empty command", spec)
		}
		cfg.MCP[name] = config.MCPServerConfig{
			Command: parts[0],
			Args:    parts[1:],
		}
	}
	return nil
}

// parseHookSpec splits "<event>:<command>" into (event, command).
// Uses `:` as the separator (not `=`) so commands containing
// equals signs stay intact. Validates the event against the set
// of known hooks.Event constants.
func parseHookSpec(spec string) (event, command string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", errors.New("empty hook spec")
	}
	i := strings.IndexByte(spec, ':')
	if i <= 0 {
		return "", "", errors.New("expected format <event>:<command>")
	}
	event = strings.TrimSpace(spec[:i])
	command = strings.TrimSpace(spec[i+1:])
	if command == "" {
		return "", "", errors.New("empty command after ':'")
	}
	// Validate against known event names. Defer to the hooks
	// package for the canonical list so this stays in sync with
	// new Event constants.
	if _, err := hooks.ParseEvent(event); err != nil {
		return "", "", err
	}
	return event, command, nil
}

// parseMCPSpec splits "<name>:<command>" into (name, command).
// Like parseHookSpec but doesn't validate against a known set —
// MCP server names are free-form.
func parseMCPSpec(spec string) (name, command string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", errors.New("empty mcp spec")
	}
	i := strings.IndexByte(spec, ':')
	if i <= 0 {
		return "", "", errors.New("expected format <name>:<command>")
	}
	name = strings.TrimSpace(spec[:i])
	command = strings.TrimSpace(spec[i+1:])
	if command == "" {
		return "", "", errors.New("empty command after ':'")
	}
	return name, command, nil
}

// parsePermissionMode maps a CLI mode string to a permission.Mode enum.
// Returns an error for unknown values; empty string returns
// (ModeDefault, false, nil) so the caller knows to skip SetMode().
func parsePermissionMode(s string) (mode permission.Mode, set bool, err error) {
	switch s {
	case "":
		return permission.ModeDefault, false, nil
	case ModePlan:
		return permission.ModePlan, true, nil
	case ModeAuto:
		return permission.ModeAuto, true, nil
	case ModeDefault:
		return permission.ModeDefault, true, nil
	case ModeBypass:
		return permission.ModeBypass, true, nil
	default:
		return 0, false, fmt.Errorf("invalid permission mode %q", s)
	}
}

// ApplyPermissionOverrides mutates eval in place using the Phase 2
// CLI flags on p: --permission-mode, --allow-tool, --deny-tool,
// --dry-run. Returns a UsageError on bad input.
//
// Order:
//  1. --dry-run aliases to --permission-mode plan (only if mode is
//     empty, so an explicit --permission-mode wins).
//  2. --permission-mode overrides eval.mode.
//  3. --allow-tool entries become session allow rules.
//  4. --deny-tool entries become session deny rules.
//
// If eval is nil (no config, no permission rules) but the user has
// any session overrides, we construct a bare evaluator so the
// overrides still take effect. The caller is responsible for
// installing the returned evaluator back onto engine params.
//
// LIMITATION (noted by CC Phase 2 review):
// permission.Check iterates all DENY rules (session + global) before
// any ALLOW rules (see permission.go:83-104). That means a
// config-level `deny bash:*` cannot be overridden by `--allow-tool
// bash:git *` on the command line — the config deny wins first.
// The same shadowing applies to built-in mode denies:
//   - `--permission-mode plan --allow-tool write` still denies
//     write because plan mode short-circuits write at line 74
//     before rule iteration.
//   - `--permission-mode auto --allow-tool <x>` DOES work because
//     auto-mode only denies at the end if no allow rule matched.
//
// Phase 2 documents these rather than restructuring the evaluator's
// iteration order, which would affect every existing user. If this
// becomes a real UX friction point, a future phase can add a
// third rule tier (e.g. "session-override") that beats global deny.
// Until then, users who need to override config denies must edit
// their config file, and users who need to override plan mode
// should use a different mode.
//
// Colon parsing note: parseToolRuleSpec splits on the FIRST colon,
// so `--allow-tool bash:echo hi:bye` yields name="bash",
// pattern="echo hi:bye". Multi-colon patterns are preserved,
// but the first colon is always the name/pattern separator.
func ApplyPermissionOverrides(eval *permission.Evaluator, p *Params, projectRoot string) (*permission.Evaluator, error) {
	mode := p.PermissionMode
	if mode == "" && p.DryRun {
		// Dry-run alias. Spec v7 §2 Permission/mode:
		// "--dry-run implies --permission-mode plan for write tools"
		mode = ModePlan
	}
	m, set, err := parsePermissionMode(mode)
	if err != nil {
		return eval, NewUsageError("%v", err)
	}

	// Build an evaluator if the config didn't give us one but we
	// have any overrides to apply. Without this, --allow-tool /
	// --deny-tool on a config-less project would silently drop.
	needEval := set || len(p.AllowTools) > 0 || len(p.DenyTools) > 0
	if eval == nil && needEval {
		eval = permission.NewEvaluator(permission.ModeDefault, projectRoot, nil)
	}
	if eval == nil {
		return nil, nil
	}

	if set {
		eval.SetMode(m)
	}
	for _, s := range p.AllowTools {
		name, pattern, perr := parseToolRuleSpec(s)
		if perr != nil {
			return eval, NewUsageError("invalid --allow-tool %q: %v", s, perr)
		}
		eval.AddSessionRule(permission.Rule{
			Tool: name, Pattern: pattern,
			Action: permission.ActionAllow, Source: "cli",
		})
	}
	for _, s := range p.DenyTools {
		name, pattern, perr := parseToolRuleSpec(s)
		if perr != nil {
			return eval, NewUsageError("invalid --deny-tool %q: %v", s, perr)
		}
		eval.AddSessionRule(permission.Rule{
			Tool: name, Pattern: pattern,
			Action: permission.ActionDeny, Source: "cli",
		})
	}
	return eval, nil
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

	// Phase 7: snapshot pre-run working tree state so --commit can
	// detect whether the user had uncommitted changes before this
	// run started. Captured BEFORE engine construction so the
	// check is accurate even if the engine's tools touch files.
	//
	// CC Phase 7 review caught: with --commit explicitly requested,
	// a git status failure (no git, not a repo, submodule error)
	// must fail the run upfront rather than silently bypassing the
	// cleanliness check. If --commit is NOT set we don't care.
	if p.Commit {
		statusCmd := osexec.CommandContext(ctx, "git", "status", "--porcelain")
		var statusErr bytes.Buffer
		statusCmd.Stderr = &statusErr
		out, err := statusCmd.Output()
		if err != nil {
			return NewUsageError(
				"--commit: pre-run `git status` failed (%v) "+
					"— not inside a git repo, or git unavailable: %s",
				err, strings.TrimSpace(statusErr.String()))
		}
		p.preRunDirty = strings.TrimSpace(string(out))
		if p.preRunDirty != "" && !p.CommitDirty {
			return NewUsageError(
				"--commit refuses to run with a dirty working tree " +
					"(pass --commit-dirty to override, but this mixes " +
					"human + agent changes in the same commit). Run " +
					"`git status` to see what's uncommitted.")
		}
		if p.preRunDirty != "" && p.CommitDirty {
			fmt.Fprintln(os.Stderr,
				"altcode: --commit-dirty bypassing clean-working-tree "+
					"check; your uncommitted changes will be in the "+
					"next commit alongside agent edits")
		}
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

	// Phase 12: event accumulator for --print-tree and --save-transcript.
	// Nil when neither flag is set so we don't pay the buffer cost
	// on the common path.
	acc := newEventAccumulator(&p)
	if acc != nil {
		// Wrap the channel in a tee so every event reaches both the
		// drain and the accumulator. We drain the tee here in the
		// main goroutine so the accumulator stays single-writer
		// and we don't need a mutex.
		teed := make(chan event.Event, 64)
		go func() {
			defer close(teed)
			for ev := range ch {
				acc.observe(ev)
				teed <- ev
			}
		}()
		ch = teed
	}

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

	// Phase 12: render tool tree + write transcript after drain
	// finishes. These run regardless of err because the user asked
	// for diagnostics and suppressing them on failure would hide
	// the very information they need.
	if acc != nil {
		if p.PrintTree {
			acc.renderTree(os.Stderr)
		}
		if p.SaveTranscript != "" {
			if terr := acc.writeTranscript(p.SaveTranscript); terr != nil && err == nil {
				err = fmt.Errorf("--save-transcript: %w", terr)
			}
		}
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

	// Phase 7: artifact + commit handling runs AFTER the drain
	// finishes but before Run returns. Errors from artifact
	// writing / commit surface as the final return error; if the
	// engine already errored we let that take precedence.
	if err == nil && (p.SaveTranscript != "" || p.SaveCost != "" || p.SaveDiff != "") {
		if aerr := writeArtifacts(ctx, &p, eng); aerr != nil {
			err = aerr
		}
	}
	if err == nil && p.Commit {
		if cerr := commitChanges(ctx, &p); cerr != nil {
			err = cerr
		}
	}
	return err
}

// writeArtifacts writes optional post-run artifact files. Phase 7
// ships the cost and diff artifacts; transcript is best-effort
// because full replay would require re-running the drain.
func writeArtifacts(ctx context.Context, p *Params, eng *engine.Engine) error {
	if p.SaveCost != "" && eng != nil {
		if ct := eng.CostTracker(); ct != nil {
			in, out := ct.TotalTokens()
			total := ct.TotalCost()
			payload := map[string]any{
				"input_tokens":  in,
				"output_tokens": out,
				"total_usd":     total,
				"turns":         ct.Turns(),
			}
			data, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal cost: %w", err)
			}
			if werr := os.WriteFile(p.SaveCost, data, 0o644); werr != nil {
				return fmt.Errorf("--save-cost %q: %w", p.SaveCost, werr)
			}
		}
	}
	if p.SaveDiff != "" {
		// Snapshot the diff of any files touched since the pre-run
		// capture. Uses `git diff HEAD` over the full tree because
		// tracking which files the engine edited would require a
		// separate event collector (Phase 12 will add that when
		// --print-tree lands).
		cmd := osexec.CommandContext(ctx, "git", "diff", "--no-color", "HEAD")
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		if rerr := cmd.Run(); rerr != nil {
			return fmt.Errorf("--save-diff git diff: %w", rerr)
		}
		if werr := os.WriteFile(p.SaveDiff, buf.Bytes(), 0o644); werr != nil {
			return fmt.Errorf("--save-diff %q: %w", p.SaveDiff, werr)
		}
	}
	// --save-transcript is handled by the Phase 12 event
	// accumulator in Run() itself, not here. writeArtifacts only
	// covers cost + diff; transcript writing happens after
	// acc.observe() has captured every event.
	return nil
}

// commitChanges runs `git commit -m <message>` after a successful
// run when --commit was set. Refuses gracefully if the run
// produced no changes (exits 0 with an info line instead of
// erroring). Auto-generates a short commit message from the
// prompt text.
//
// Staging is SCOPED: only paths that appeared or changed between
// preRunDirty and the post-run `git status --porcelain` are
// staged explicitly via `git add -- <paths>`. This prevents the
// `git add -A` blast radius that CC Phase 7 review caught —
// otherwise --commit would sweep every untracked file in the
// repo (screenshots, .env, build artifacts) into the commit.
//
// --commit-dirty relaxes the scope: pre-run dirty paths are
// also staged, so the commit contains both human and agent work.
//
// Cancellation atomicity: Codex Phase 7 review caught that if
// ctx cancels between `git add` and `git commit`, the index is
// left staged with no commit. We use context.Background for the
// git sub-commands here so a late SIGINT/SIGTERM doesn't wedge
// the working tree between stages. The caller already ran the
// full engine loop and received Done before we got here.
func commitChanges(_ context.Context, p *Params) error {
	ctx := context.Background()
	statusCmd := osexec.CommandContext(ctx, "git", "status", "--porcelain")
	var statusErr bytes.Buffer
	statusCmd.Stderr = &statusErr
	out, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("--commit: post-run git status failed: %w: %s",
			err, strings.TrimSpace(statusErr.String()))
	}
	postRun := strings.TrimSpace(string(out))
	if postRun == "" {
		fmt.Fprintln(os.Stderr, "altcode: --commit: no changes to commit")
		return nil
	}

	// Compute the delta: paths that the engine actually touched.
	// Without this, a user with screenshots in the repo root would
	// see every screenshot swept into the "agent" commit. The
	// porcelain format is "XY path\n" where XY is the two-char
	// status code.
	preRunPaths := porcelainPaths(p.preRunDirty)
	postRunPaths := porcelainPaths(postRun)
	var toStage []string
	for path := range postRunPaths {
		// In --commit-dirty mode, stage everything (pre-run + agent).
		// Otherwise stage only paths the engine touched.
		if p.CommitDirty || !preRunPaths[path] {
			toStage = append(toStage, path)
		}
	}
	// Paths that were in preRunDirty but NOT in postRun mean the
	// engine reverted them — those don't need explicit staging.
	if len(toStage) == 0 {
		fmt.Fprintln(os.Stderr, "altcode: --commit: no agent-edited files to commit")
		return nil
	}

	// git add -- <paths>. Capture stderr so failures are actionable.
	addArgs := append([]string{"add", "--"}, toStage...)
	addCmd := osexec.CommandContext(ctx, "git", addArgs...)
	var addStderr bytes.Buffer
	addCmd.Stderr = &addStderr
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("--commit: git add failed: %w: %s",
			err, strings.TrimSpace(addStderr.String()))
	}

	msg := generateCommitMessage(p.Prompt)
	commitCmd := osexec.CommandContext(ctx, "git", "commit", "-m", msg)
	var commitStderr bytes.Buffer
	commitCmd.Stderr = &commitStderr
	if err := commitCmd.Run(); err != nil {
		return fmt.Errorf("--commit: git commit failed: %w\n%s",
			err, strings.TrimSpace(commitStderr.String()))
	}
	fmt.Fprintf(os.Stderr, "altcode: committed %d file(s) with message: %s\n",
		len(toStage), msg)
	return nil
}

// porcelainPaths parses `git status --porcelain` output into a
// set of filesystem paths. Each line is "XY path" or "XY path -> renamed_path".
// For renamed entries we use the NEW path (right side of the arrow)
// because that's what the post-run actually reflects.
func porcelainPaths(porcelain string) map[string]bool {
	if porcelain == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) < 4 {
			continue
		}
		// Skip status chars (2) + space (1) = 3 chars of prefix.
		path := line[3:]
		// Rename entries: "XY old -> new". Use the new path.
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		out[path] = true
	}
	return out
}

// generateCommitMessage builds a conventional-commit-ish message
// from the prompt text. Keeps it under 72 characters (first line)
// and tags with [altcode] so humans can distinguish agent commits
// from their own. Never uses the prompt content verbatim — a
// carefully-worded commit title is usually cleaner than truncating
// raw prompt text.
func generateCommitMessage(prompt string) string {
	// First line of the prompt, trimmed and truncated.
	first := prompt
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	first = strings.TrimSpace(first)
	if first == "" {
		first = "agent commit"
	}
	const maxLen = 60
	if len(first) > maxLen {
		first = first[:maxLen-3] + "..."
	}
	return fmt.Sprintf("[altcode] %s", first)
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
		case event.BudgetExceeded:
			// Phase 8: print the budget-exceeded reason to stderr so
			// headless users can tell "ran out of turns" or "hit the
			// cost cap" apart from a normal completion. The engine
			// sends BudgetExceeded synchronously before Done via its
			// top-level defer, so ordering is preserved; drain exits
			// when it receives Done and the channel closes.
			if ev.Info != "" && !p.Quiet {
				fmt.Fprintf(os.Stderr, "%saltcode: %s%s\n", dim, ev.Info, reset)
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
