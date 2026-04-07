# Team Orchestration v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a hybrid workflow orchestrator that dispatches tasks to external CLI tools (codex/claude/opencode) through declarative phased workflows with split-pane TUI display and manual override.

**Architecture:** Two new packages (`wfdef` for workflow definitions, `orchestra` for phase execution) plus modifications to existing `agent/external.go` (typed events) and `tui/` (workflow header + phase transitions). The orchestra reads YAML workflow files, spawns agents via `agent.SpawnExternal`, streams typed events to TUI, and supports pause/skip/inject/abort at phase boundaries.

**Tech Stack:** Go 1.22, Bubbletea TUI, YAML frontmatter parsing, `os/exec` for CLI spawning, `encoding/json` for stream-json parsing.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/wfdef/wfdef.go` | Create | Workflow definition types, YAML parser, Discover |
| `internal/wfdef/wfdef_test.go` | Create | Parse round-trip, validation tests |
| `internal/agent/external.go` | Modify | Upgrade to typed AgentEvent, add trySend, claude control_request |
| `internal/agent/external_test.go` | Modify | Update tests for typed events |
| `internal/orchestra/orchestra.go` | Create | Phase engine: Run loop, topo sort, event fan-in |
| `internal/orchestra/parser.go` | Create | Claude stream-json + codex line parsers |
| `internal/orchestra/parser_test.go` | Create | Parser unit tests with fixture data |
| `internal/orchestra/context.go` | Create | Provider-aware workdir prep + context injection |
| `internal/orchestra/control.go` | Create | OverrideCmd types |
| `internal/orchestra/orchestra_test.go` | Create | Phase sequencing tests with mock backends |
| `internal/tui/workflow_header.go` | Create | Phase breadcrumb renderer |
| `internal/tui/workflow_run.go` | Modify | Wire orchestra events to TUI, phase transitions |
| `internal/tui/app.go` | Modify | Add orchestraRun field, Update/View integration |
| `internal/tui/commands.go` | Modify | `/workflow <name> <task>` command |
| `.altcode/workflows/ship-feature.md` | Create | Default feature-dev workflow |
| `.altcode/workflows/review.md` | Create | Default review workflow |
| `.altcode/workflows/fix.md` | Create | Default fix workflow |

---

### Task 1: Workflow Definition Types + Parser (`wfdef`)

**Files:**
- Create: `internal/wfdef/wfdef.go`
- Create: `internal/wfdef/wfdef_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/wfdef/wfdef_test.go
package wfdef

import (
	"testing"
	"time"
)

const testWorkflow = `---
name: test-flow
description: A test workflow
phases:
  - name: plan
    agents:
      - role: planner
        backend: claude
        model: claude-sonnet-4-20250514
        prompt: "Plan: {{.Task}}"
    timeout: 5m
    required: true
  - name: implement
    depends_on: [plan]
    agents:
      - role: coder
        backend: codex
    timeout: 10m
  - name: review
    depends_on: [implement]
    parallel: true
    on_failure: human
    agents:
      - role: reviewer
        backend: claude
      - role: challenger
        backend: codex
---
Default test workflow.
`

func TestParseWorkflow(t *testing.T) {
	def, err := Parse([]byte(testWorkflow))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if def.Name != "test-flow" {
		t.Errorf("Name = %q, want %q", def.Name, "test-flow")
	}
	if len(def.Phases) != 3 {
		t.Fatalf("Phases = %d, want 3", len(def.Phases))
	}
	// Phase 0: plan
	p := def.Phases[0]
	if p.Name != "plan" || !p.Required || p.Timeout != 5*time.Minute {
		t.Errorf("phase 0: name=%q required=%v timeout=%v", p.Name, p.Required, p.Timeout)
	}
	if len(p.Agents) != 1 || p.Agents[0].Role != "planner" || p.Agents[0].Backend != "claude" {
		t.Errorf("phase 0 agents: %+v", p.Agents)
	}
	// Phase 1: implement
	if def.Phases[1].DependsOn[0] != "plan" {
		t.Errorf("phase 1 depends_on = %v", def.Phases[1].DependsOn)
	}
	// Phase 2: review
	if !def.Phases[2].Parallel || def.Phases[2].OnFailure != FailureHuman {
		t.Errorf("phase 2: parallel=%v on_failure=%v", def.Phases[2].Parallel, def.Phases[2].OnFailure)
	}
	if len(def.Phases[2].Agents) != 2 {
		t.Errorf("phase 2 agents = %d, want 2", len(def.Phases[2].Agents))
	}
}

func TestParseWorkflow_Invalid(t *testing.T) {
	_, err := Parse([]byte("not yaml at all"))
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestTopoSort(t *testing.T) {
	def, _ := Parse([]byte(testWorkflow))
	order, err := def.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	// plan must come before implement, implement before review
	idx := map[string]int{}
	for i, name := range order {
		idx[name] = i
	}
	if idx["plan"] >= idx["implement"] || idx["implement"] >= idx["review"] {
		t.Errorf("bad order: %v", order)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOFLAGS=-mod=mod go test ./internal/wfdef/... -v -run TestParse -count=1`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write the implementation**

```go
// internal/wfdef/wfdef.go
package wfdef

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type WorkflowDef struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Phases      []PhaseDef `yaml:"phases"`
	Path        string     `yaml:"-"`
}

type PhaseDef struct {
	Name      string            `yaml:"name"`
	DependsOn []string          `yaml:"depends_on"`
	Parallel  bool              `yaml:"parallel"`
	Agents    []AgentAssignment `yaml:"agents"`
	Timeout   time.Duration     `yaml:"timeout"`
	Required  bool              `yaml:"required"`
	OnFailure FailurePolicy     `yaml:"on_failure"`
	Condition string            `yaml:"condition"`
}

type AgentAssignment struct {
	Role    string `yaml:"role"`
	Backend string `yaml:"backend"`
	Model   string `yaml:"model"`
	Prompt  string `yaml:"prompt"`
}

type FailurePolicy string

const (
	FailureRetry FailurePolicy = "retry"
	FailureSkip  FailurePolicy = "skip"
	FailureAbort FailurePolicy = "abort"
	FailureHuman FailurePolicy = "human"
)

// Parse reads a workflow definition from markdown with YAML frontmatter.
func Parse(data []byte) (*WorkflowDef, error) {
	fm, err := extractFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var def WorkflowDef
	if err := yaml.Unmarshal(fm, &def); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}
	if def.Name == "" {
		return nil, fmt.Errorf("workflow name is required")
	}
	// Default required to true
	for i := range def.Phases {
		if def.Phases[i].OnFailure == "" {
			def.Phases[i].OnFailure = FailureAbort
		}
	}
	return &def, nil
}

// ParseFile reads a workflow from a markdown file.
func ParseFile(path string) (*WorkflowDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	def, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	def.Path = path
	return def, nil
}

// Discover finds all workflow files in the given directories.
func Discover(dirs ...string) ([]*WorkflowDef, error) {
	var defs []*WorkflowDef
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			def, err := ParseFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue // skip invalid files
			}
			defs = append(defs, def)
		}
	}
	return defs, nil
}

// TopoSort returns phase names in dependency order.
func (d *WorkflowDef) TopoSort() ([]string, error) {
	phaseMap := map[string]*PhaseDef{}
	for i := range d.Phases {
		phaseMap[d.Phases[i].Name] = &d.Phases[i]
	}
	visited := map[string]bool{}
	visiting := map[string]bool{}
	var order []string

	var visit func(name string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("cycle detected at phase %q", name)
		}
		visiting[name] = true
		p, ok := phaseMap[name]
		if !ok {
			return fmt.Errorf("unknown phase %q", name)
		}
		for _, dep := range p.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		order = append(order, name)
		return nil
	}

	for _, p := range d.Phases {
		if err := visit(p.Name); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// PhaseByName returns the phase definition with the given name.
func (d *WorkflowDef) PhaseByName(name string) *PhaseDef {
	for i := range d.Phases {
		if d.Phases[i].Name == name {
			return &d.Phases[i]
		}
	}
	return nil
}

func extractFrontmatter(data []byte) ([]byte, error) {
	const sep = "---"
	s := string(data)
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, sep) {
		return nil, fmt.Errorf("no YAML frontmatter found")
	}
	rest := s[len(sep):]
	end := strings.Index(rest, "\n"+sep)
	if end < 0 {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	fm := rest[:end]
	return bytes.TrimSpace([]byte(fm)), nil
}
```

- [ ] **Step 4: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/wfdef/... -v -count=1`
Expected: PASS (all 3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/wfdef/
git commit -m "feat(wfdef): workflow definition types with YAML frontmatter parser"
```

---

### Task 2: Upgrade agent/external.go — Typed Events + Claude Control

**Files:**
- Modify: `internal/agent/external.go`
- Modify: `internal/agent/external_test.go`

- [ ] **Step 1: Write test for typed events**

```go
// Add to internal/agent/external_test.go
func TestAgentEventTypes(t *testing.T) {
	// Verify all event types are distinct
	types := []AgentEventType{EventText, EventThinking, EventToolUse, EventToolResult, EventStatus, EventError}
	seen := map[AgentEventType]bool{}
	for _, et := range types {
		if seen[et] {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = true
	}
}
```

- [ ] **Step 2: Add AgentEvent types to external.go**

Replace the `ExternalAgentStream` definition and add typed events. Add `trySend()` helper. Add `SessionID` and `SystemPrompt` to `ExternalAgentConfig`. Update `buildCLICommand` for claude to add `--max-turns`, `--append-system-prompt`, `--resume`. Add claude control_request handling via stdin pipe.

Key changes:
- `Lines <-chan string` → `Events <-chan AgentEvent`
- New `AgentEvent` struct with `Type AgentEventType`
- New `AgentEventType` enum: text, thinking, tool_use, tool_result, status, error
- `ExternalAgentResult` gains `SessionID string`
- `ExternalAgentConfig` gains `SessionID string`, `SystemPrompt string`, `MaxTurns int`
- `buildCLICommand` for claude: add `--output-format stream-json`, `--max-turns`, `--append-system-prompt`, `--resume`
- `SpawnExternal` goroutine: parse claude stream-json into typed events; handle `control_request` via stdin
- `trySend[T any]()` non-blocking send (multica pattern A2)

- [ ] **Step 3: Update SpawnExternal to use typed events + stdin pipe**

The goroutine that reads stdout should:
1. Open both stdout and stdin pipes
2. Scan lines; for claude backend, JSON-parse each line
3. Map claude `{"type":"assistant"}` to `EventText`, `{"type":"result"}` to `EventStatus`, etc.
4. Handle `{"type":"control_request"}` by writing approval JSON to stdin
5. For non-claude backends, emit raw lines as `EventText`
6. Use `trySend()` for all channel sends
7. Accumulate output in `strings.Builder` regardless of channel state

- [ ] **Step 4: Update existing tests**

Fix `TestSpawnExternal_Timeout` and `TestSpawnTeam` to read `Events` channel instead of `Lines`. Fix `TestBuildCLICommand_Claude` to check new flags.

- [ ] **Step 5: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/agent/... -v -count=1 -timeout=30s`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent/external.go internal/agent/external_test.go
git commit -m "feat(agent): typed streaming events, claude control_request, trySend"
```

---

### Task 3: Stream Parsers (`orchestra/parser.go`)

**Files:**
- Create: `internal/orchestra/parser.go`
- Create: `internal/orchestra/parser_test.go`

- [ ] **Step 1: Write parser tests with real fixture data**

```go
// internal/orchestra/parser_test.go
package orchestra

import "testing"

func TestParseClaudeStreamJSON_Text(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != KindText || ev.Text != "Hello world" {
		t.Errorf("got type=%s text=%q", ev.Type, ev.Text)
	}
}

func TestParseClaudeStreamJSON_ToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","id":"call_1","input":{"path":"/tmp"}}]}}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != KindToolStart || ev.Tool != "Read" {
		t.Errorf("got type=%s tool=%q", ev.Type, ev.Tool)
	}
}

func TestParseClaudeStreamJSON_Result(t *testing.T) {
	line := `{"type":"result","result_text":"Done","session_id":"sess-123"}`
	ev := ParseClaudeStreamJSON(line)
	if ev == nil {
		t.Fatal("expected event")
	}
	if ev.Type != KindPhaseDone {
		t.Errorf("got type=%s, want phase_done", ev.Type)
	}
}

func TestParseClaudeStreamJSON_Invalid(t *testing.T) {
	ev := ParseClaudeStreamJSON("not json")
	if ev != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestParseCodexLine(t *testing.T) {
	tests := []struct {
		line string
		kind PhaseEventKind
	}{
		{"[Read] /tmp/foo.go", KindToolStart},
		{"✓", KindToolDone},
		{"Error: something failed", KindError},
		{"regular output text", KindText},
	}
	for _, tt := range tests {
		ev := ParseCodexLine(tt.line)
		if ev.Type != tt.kind {
			t.Errorf("ParseCodexLine(%q) = %s, want %s", tt.line, ev.Type, tt.kind)
		}
	}
}
```

- [ ] **Step 2: Implement parsers**

```go
// internal/orchestra/parser.go
package orchestra

import (
	"encoding/json"
	"strings"
)

// PhaseEventKind identifies the type of event from a phase.
type PhaseEventKind string

const (
	KindText      PhaseEventKind = "text"
	KindToolStart PhaseEventKind = "tool_start"
	KindToolDone  PhaseEventKind = "tool_done"
	KindThinking  PhaseEventKind = "thinking"
	KindError     PhaseEventKind = "error"
	KindPhaseDone PhaseEventKind = "phase_done"
)

// PhaseEvent is a typed event from a running phase agent.
type PhaseEvent struct {
	Phase     string
	Role      string
	Type      PhaseEventKind
	Text      string
	Tool      string
	SessionID string
}

// ParseClaudeStreamJSON parses one line of claude --output-format stream-json.
func ParseClaudeStreamJSON(line string) *PhaseEvent {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}
	var msgType string
	if t, ok := raw["type"]; ok {
		json.Unmarshal(t, &msgType)
	}

	switch msgType {
	case "assistant":
		return parseAssistantMessage(raw)
	case "result":
		var r struct {
			ResultText string `json:"result_text"`
			SessionID  string `json:"session_id"`
		}
		json.Unmarshal([]byte(line), &r)
		return &PhaseEvent{Type: KindPhaseDone, Text: r.ResultText, SessionID: r.SessionID}
	case "system":
		return &PhaseEvent{Type: KindText, Text: "[system]"}
	default:
		return nil
	}
}

func parseAssistantMessage(raw map[string]json.RawMessage) *PhaseEvent {
	var msg struct {
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				Name  string          `json:"name"`
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	data, _ := json.Marshal(raw)
	if json.Unmarshal(data, &msg) != nil {
		return nil
	}
	for _, block := range msg.Message.Content {
		switch block.Type {
		case "text":
			return &PhaseEvent{Type: KindText, Text: block.Text}
		case "thinking":
			return &PhaseEvent{Type: KindThinking, Text: block.Text}
		case "tool_use":
			return &PhaseEvent{Type: KindToolStart, Tool: block.Name, Text: block.ID}
		case "tool_result":
			return &PhaseEvent{Type: KindToolDone, Tool: block.Name}
		}
	}
	return nil
}

// ParseCodexLine heuristically classifies a raw codex output line.
func ParseCodexLine(line string) *PhaseEvent {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "[") && strings.Contains(trimmed, "]"):
		// [ToolName] detail
		end := strings.Index(trimmed, "]")
		tool := trimmed[1:end]
		return &PhaseEvent{Type: KindToolStart, Tool: tool, Text: trimmed}
	case trimmed == "✓" || strings.HasPrefix(trimmed, "✓ "):
		return &PhaseEvent{Type: KindToolDone, Text: trimmed}
	case strings.HasPrefix(strings.ToLower(trimmed), "error"):
		return &PhaseEvent{Type: KindError, Text: trimmed}
	default:
		return &PhaseEvent{Type: KindText, Text: trimmed}
	}
}
```

- [ ] **Step 3: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/orchestra/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/orchestra/
git commit -m "feat(orchestra): claude stream-json + codex line parsers"
```

---

### Task 4: Context Injection (`orchestra/context.go`)

**Files:**
- Create: `internal/orchestra/context.go`

- [ ] **Step 1: Implement context injection**

Provider-aware context file writing following multica's execenv pattern:

```go
// internal/orchestra/context.go
package orchestra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InjectContext writes provider-native context files into the agent's workdir.
// For claude: appends to CLAUDE.md. For codex/opencode: writes AGENTS.md.
func InjectContext(workDir, provider, role, task string, priorOutputs map[string]string) error {
	content := buildContextContent(role, task, priorOutputs)
	switch provider {
	case "claude":
		return appendOrCreate(filepath.Join(workDir, "CLAUDE.md"), content)
	case "codex", "opencode":
		return appendOrCreate(filepath.Join(workDir, "AGENTS.md"), content)
	default:
		return appendOrCreate(filepath.Join(workDir, "AGENTS.md"), content)
	}
}

func buildContextContent(role, task string, priorOutputs map[string]string) string {
	var b strings.Builder
	b.WriteString("# Altcode Workflow Context\n\n")
	fmt.Fprintf(&b, "**Role:** %s\n\n", role)
	fmt.Fprintf(&b, "**Task:** %s\n\n", task)

	if len(priorOutputs) > 0 {
		b.WriteString("## Prior Phase Outputs\n\n")
		for phase, output := range priorOutputs {
			fmt.Fprintf(&b, "### %s\n\n", phase)
			// Truncate to 32KB
			if len(output) > 32768 {
				output = output[:32768] + "\n\n[truncated]"
			}
			b.WriteString(output)
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

func appendOrCreate(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n" + content)
	return err
}
```

- [ ] **Step 2: Run build**

Run: `GOFLAGS=-mod=mod go build ./internal/orchestra/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/orchestra/context.go
git commit -m "feat(orchestra): provider-aware context injection"
```

---

### Task 5: Override Control Types (`orchestra/control.go`)

**Files:**
- Create: `internal/orchestra/control.go`

- [ ] **Step 1: Implement control types**

```go
// internal/orchestra/control.go
package orchestra

// OverrideCmd is injected by the TUI operator during workflow execution.
type OverrideCmd struct {
	Op      OverrideOp
	Target  string // role name, "" = all
	Message string // for OpInject
}

// OverrideOp identifies the override operation.
type OverrideOp string

const (
	OpPause  OverrideOp = "pause"
	OpResume OverrideOp = "resume"
	OpSkip   OverrideOp = "skip"
	OpInject OverrideOp = "inject"
	OpAbort  OverrideOp = "abort"
)

// Verdict represents the outcome of a phase.
type Verdict int

const (
	VerdictPass Verdict = iota
	VerdictFail
	VerdictTimeout
	VerdictSkipped
)

func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "pass"
	case VerdictFail:
		return "fail"
	case VerdictTimeout:
		return "timeout"
	case VerdictSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// PhaseResult holds the outcome of a completed phase.
type PhaseResult struct {
	PhaseID   string
	Verdict   Verdict
	Outputs   map[string]string // role → output text
	SessionID string            // last agent's session ID for resume
}
```

- [ ] **Step 2: Build**

Run: `GOFLAGS=-mod=mod go build ./internal/orchestra/...`

- [ ] **Step 3: Commit**

```bash
git add internal/orchestra/control.go
git commit -m "feat(orchestra): override control types and verdict enum"
```

---

### Task 6: Phase Engine (`orchestra/orchestra.go`)

**Files:**
- Create: `internal/orchestra/orchestra.go`
- Create: `internal/orchestra/orchestra_test.go`

- [ ] **Step 1: Write test for phase sequencing**

```go
// internal/orchestra/orchestra_test.go
package orchestra

import (
	"context"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/wfdef"
)

func TestRunWorkflow_SinglePhase(t *testing.T) {
	def := &wfdef.WorkflowDef{
		Name: "test",
		Phases: []wfdef.PhaseDef{{
			Name:    "echo",
			Agents:  []wfdef.AgentAssignment{{Role: "worker", Backend: "echo"}},
			Timeout: 5 * time.Second,
		}},
	}

	events := make(chan PhaseEvent, 100)
	override := make(chan OverrideCmd)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := Run(ctx, RunParams{
		Def:      def,
		Task:     "hello",
		WorkDir:  t.TempDir(),
		Events:   events,
		Override: override,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should get at least one phase_done event
	found := false
	close(events) // drain
	for ev := range events {
		if ev.Type == KindPhaseDone {
			found = true
		}
	}
	if !found {
		t.Error("expected KindPhaseDone event")
	}
}
```

- [ ] **Step 2: Implement the phase engine**

The `Run` function:
1. Topological sort phases
2. For each phase in order: check override channel, inject context, spawn agents, collect events, evaluate verdict
3. Emit `PhaseEvent` for each line/tool/error from agents
4. Emit `KindPhaseDone` when phase completes

```go
// internal/orchestra/orchestra.go
package orchestra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/agent"
	"github.com/altcode-ai/altcode/internal/wfdef"
)

// RunParams configures a workflow execution.
type RunParams struct {
	Def      *wfdef.WorkflowDef
	Task     string
	WorkDir  string
	Events   chan<- PhaseEvent
	Override <-chan OverrideCmd
}

// Run executes a workflow definition phase by phase.
func Run(ctx context.Context, p RunParams) error {
	order, err := p.Def.TopoSort()
	if err != nil {
		return fmt.Errorf("topo sort: %w", err)
	}

	results := map[string]*PhaseResult{}

	for _, phaseName := range order {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Check for override before starting phase
		select {
		case cmd := <-p.Override:
			if cmd.Op == OpAbort {
				return fmt.Errorf("workflow aborted by user")
			}
			if cmd.Op == OpSkip {
				trySendEvent(p.Events, PhaseEvent{Phase: phaseName, Type: KindPhaseDone, Text: "skipped"})
				results[phaseName] = &PhaseResult{PhaseID: phaseName, Verdict: VerdictSkipped}
				continue
			}
		default:
		}

		phase := p.Def.PhaseByName(phaseName)
		if phase == nil {
			continue
		}

		// Check dependencies passed
		for _, dep := range phase.DependsOn {
			if r, ok := results[dep]; ok && r.Verdict == VerdictFail {
				trySendEvent(p.Events, PhaseEvent{Phase: phaseName, Type: KindPhaseDone, Text: "skipped (dependency failed)"})
				results[phaseName] = &PhaseResult{PhaseID: phaseName, Verdict: VerdictSkipped}
				continue
			}
		}

		// Build prior outputs for context injection
		priorOutputs := map[string]string{}
		for name, r := range results {
			for role, out := range r.Outputs {
				priorOutputs[name+"/"+role] = out
			}
		}

		result := runPhase(ctx, p, phase, priorOutputs)
		results[phaseName] = result

		trySendEvent(p.Events, PhaseEvent{
			Phase: phaseName,
			Type:  KindPhaseDone,
			Text:  result.Verdict.String(),
		})

		if result.Verdict == VerdictFail && phase.OnFailure == wfdef.FailureAbort {
			return fmt.Errorf("phase %q failed", phaseName)
		}
	}
	return nil
}

func runPhase(ctx context.Context, p RunParams, phase *wfdef.PhaseDef, priorOutputs map[string]string) *PhaseResult {
	timeout := phase.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	phaseCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	outputs := map[string]string{}
	var lastSessionID string

	for _, ag := range phase.Agents {
		// Inject context
		InjectContext(p.WorkDir, ag.Backend, ag.Role, p.Task, priorOutputs)

		prompt := ag.Prompt
		if prompt == "" {
			prompt = p.Task
		}
		// Simple template expansion
		prompt = strings.ReplaceAll(prompt, "{{.Task}}", p.Task)

		cfg := agent.ExternalAgentConfig{
			Backend: agent.CLIBackend(ag.Backend),
			Role:    ag.Role,
			Model:   ag.Model,
			Timeout: timeout,
			WorkDir: p.WorkDir,
		}

		stream := agent.SpawnExternal(phaseCtx, cfg, prompt)

		// Drain events
		var output strings.Builder
		for ev := range stream.Events {
			output.WriteString(ev.Content + "\n")
			trySendEvent(p.Events, PhaseEvent{
				Phase: phase.Name,
				Role:  ag.Role,
				Type:  mapAgentEvent(ev.Type),
				Text:  ev.Content,
				Tool:  ev.Tool,
			})
		}

		result := <-stream.Result
		outputs[ag.Role] = result.Output
		if result.SessionID != "" {
			lastSessionID = result.SessionID
		}

		if result.Error != nil {
			return &PhaseResult{
				PhaseID: phase.Name, Verdict: VerdictFail,
				Outputs: outputs, SessionID: lastSessionID,
			}
		}
	}

	return &PhaseResult{
		PhaseID: phase.Name, Verdict: VerdictPass,
		Outputs: outputs, SessionID: lastSessionID,
	}
}

func mapAgentEvent(t agent.AgentEventType) PhaseEventKind {
	switch t {
	case agent.EventText:
		return KindText
	case agent.EventThinking:
		return KindThinking
	case agent.EventToolUse:
		return KindToolStart
	case agent.EventToolResult:
		return KindToolDone
	case agent.EventError:
		return KindError
	default:
		return KindText
	}
}

func trySendEvent(ch chan<- PhaseEvent, ev PhaseEvent) {
	select {
	case ch <- ev:
	default:
	}
}
```

- [ ] **Step 3: Run tests**

Run: `GOFLAGS=-mod=mod go test ./internal/orchestra/... -v -count=1 -timeout=30s`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/orchestra/
git commit -m "feat(orchestra): phase engine with topo sort, context injection, event streaming"
```

---

### Task 7: TUI Workflow Header + Phase Wiring

**Files:**
- Create: `internal/tui/workflow_header.go`
- Modify: `internal/tui/workflow_run.go`
- Modify: `internal/tui/app.go`
- Modify: `internal/tui/commands.go`

- [ ] **Step 1: Create workflow header renderer**

```go
// internal/tui/workflow_header.go
package tui

import (
	"fmt"
	"strings"

	"github.com/altcode-ai/altcode/internal/orchestra"
	"github.com/charmbracelet/lipgloss"
)

type workflowHeader struct {
	phases  []phaseDisplay
	width   int
}

type phaseDisplay struct {
	Name    string
	Verdict orchestra.Verdict
	Active  bool
}

func (wh *workflowHeader) Render(theme Theme) string {
	if len(wh.phases) == 0 {
		return ""
	}
	var parts []string
	for _, p := range wh.phases {
		icon := "·"
		color := theme.Muted
		switch {
		case p.Active:
			icon = "⟳"
			color = theme.Warning
		case p.Verdict == orchestra.VerdictPass:
			icon = "✓"
			color = theme.Success
		case p.Verdict == orchestra.VerdictFail:
			icon = "✗"
			color = theme.Error
		case p.Verdict == orchestra.VerdictSkipped:
			icon = "⊘"
			color = theme.Muted
		}
		badge := lipgloss.NewStyle().Foreground(color).Render(
			fmt.Sprintf("[%s %s]", p.Name, icon))
		parts = append(parts, badge)
	}
	sep := lipgloss.NewStyle().Foreground(theme.Muted).Render(" → ")
	line := strings.Join(parts, sep)
	return lipgloss.NewStyle().Width(wh.width).Render("  " + line)
}
```

- [ ] **Step 2: Rewrite workflow_run.go to use orchestra**

Replace the existing `startTeamRun` to use `orchestra.Run` when a workflow definition is found, falling back to direct agent spawning otherwise. Add `feedWorkflowRun` goroutine that reads `PhaseEvent` and sends `workflowPhaseTick` tea.Msg. Add phase transition handling that calls `teamView.Start()` with new roles per phase.

- [ ] **Step 3: Wire into app.go**

Add `workflowHeader *workflowHeader` field. In `View()`, render the header between the main header and the team panes when workflow is active. In `Update()`, handle `workflowPhaseTick` to advance the header and reset team panes.

- [ ] **Step 4: Update commands.go**

Make `/workflow <name> <task>` look up a workflow definition via `wfdef.Discover()`, then call `startWorkflowRun(def, task)`. Fall back to existing workflow modes if no definition file matches.

- [ ] **Step 5: Build and test**

Run: `GOFLAGS=-mod=mod go build ./... && GOFLAGS=-mod=mod go vet ./...`
Run: `GOFLAGS=-mod=mod go test ./internal/tui/... ./internal/orchestra/... ./internal/wfdef/... -race -count=1 -timeout=30s`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/tui/workflow_header.go internal/tui/workflow_run.go internal/tui/app.go internal/tui/commands.go
git commit -m "feat(tui): workflow header breadcrumb + orchestra integration"
```

---

### Task 8: Default Workflow Files

**Files:**
- Create: `.altcode/workflows/ship-feature.md`
- Create: `.altcode/workflows/review.md`
- Create: `.altcode/workflows/fix.md`

- [ ] **Step 1: Create ship-feature workflow**

```markdown
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
End-to-end feature development: design → implement → adversarial review.
```

- [ ] **Step 2: Create review workflow**

```markdown
---
name: review
description: Parallel adversarial code review
phases:
  - name: review
    parallel: true
    agents:
      - role: reviewer
        backend: claude
        prompt: "Review the codebase for bugs, security issues, and design problems: {{.Task}}"
      - role: challenger
        backend: codex
        prompt: "Be adversarial. Find race conditions, edge cases, missing tests: {{.Task}}"
    timeout: 10m
---
Parallel code review with two independent reviewers.
```

- [ ] **Step 3: Create fix workflow**

```markdown
---
name: fix
description: Diagnose, fix, and verify a bug
phases:
  - name: diagnose
    agents:
      - role: investigator
        backend: claude
        prompt: "Investigate and diagnose the root cause: {{.Task}}"
    timeout: 5m
    required: true
  - name: fix
    depends_on: [diagnose]
    agents:
      - role: fixer
        backend: codex
        prompt: "Fix the bug and write a regression test: {{.Task}}"
    timeout: 15m
    required: true
  - name: verify
    depends_on: [fix]
    agents:
      - role: verifier
        backend: claude
        prompt: "Run tests and verify the fix is correct. Check for regressions."
    timeout: 5m
---
Bug fix workflow: diagnose → fix → verify.
```

- [ ] **Step 4: Commit**

```bash
git add .altcode/workflows/
git commit -m "feat: default workflow definitions (ship-feature, review, fix)"
```

---

### Task 9: Full Integration Test + Pre-Push Gate

**Files:**
- No new files

- [ ] **Step 1: Clean model-generated junk**

```bash
rm -f internal/main.go internal/stringxor.go internal/reverse_test.go
rm -rf internal/lru internal/middleware internal/stack internal/ratelimit internal/datastructures
for f in internal/*.go; do pkg=$(head -1 "$f" 2>/dev/null | awk '{print $2}'); [ "$pkg" = "main" ] && rm "$f"; done
```

- [ ] **Step 2: Build**

Run: `GOFLAGS=-mod=mod go build ./...`
Expected: PASS

- [ ] **Step 3: Vet**

Run: `GOFLAGS=-mod=mod go vet ./...`
Expected: PASS

- [ ] **Step 4: Full test suite**

Run: `GOFLAGS=-mod=mod go test ./internal/... -race -count=1 -timeout=180s`
Expected: All packages PASS

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: team orchestration v2 — external CLI workflows with split-pane TUI"
```
