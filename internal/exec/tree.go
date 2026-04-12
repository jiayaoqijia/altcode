package exec

// Phase 12: event accumulator + ASCII tool-tree renderer.
//
// The accumulator buffers ToolStart/ToolResult/ErrorEvent/Done events
// during drain. At Done, it can render a flat ASCII tool tree to
// stderr (for --print-tree) and write a JSONL transcript to a file
// (for --save-transcript, which was Phase 7 but needed this
// machinery).
//
// Flat rendering only: subagent tool calls are rendered at the top
// level with a [role] prefix (e.g. `[explorer] grep "foo"`). True
// parent/child nesting needs a `parent_id` or depth field on
// `event.ToolCall` which would be a wider schema change — deferred
// to a future phase.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/altcode-ai/altcode/internal/event"
)

// eventRecord is a minimal copy of event.Event that drops the
// non-serializable response channel from PermissionRequest. Used
// both for --print-tree rendering and --save-transcript output.
type eventRecord struct {
	Type       event.EventType `json:"type"`
	Text       string          `json:"text,omitempty"`
	ToolCall   *event.ToolCall `json:"tool_call,omitempty"`
	ToolResult *event.Result   `json:"tool_result,omitempty"`
	Error      string          `json:"error,omitempty"`
	Usage      *event.UsageInfo `json:"usage,omitempty"`
	Thinking   string          `json:"thinking,omitempty"`
	Info       string          `json:"info,omitempty"`
	Permission *permRecord     `json:"permission,omitempty"`
	Timestamp  time.Time       `json:"ts"`
}

// permRecord is a JSON-safe copy of event.PermReq (no channel).
type permRecord struct {
	ToolName string `json:"tool_name"`
	Pattern  string `json:"pattern"`
}

// toolCallRecord tracks a single tool call for the tree renderer.
// Paired by ID so ToolStart and ToolResultEvent match up correctly
// even when tool calls interleave.
type toolCallRecord struct {
	ID      string
	Name    string
	Input   []byte
	Output  string
	Error   string
	Elapsed time.Duration
	StartAt time.Time
}

// eventAccumulator collects events during a drain. Concurrency-safe
// only to the extent that a single drain goroutine is the sole
// writer. Readers (render, writeTranscript) should only be called
// after the drain's event channel has closed.
type eventAccumulator struct {
	events []eventRecord
	tools  []*toolCallRecord
	byID   map[string]*toolCallRecord
}

// newEventAccumulator constructs an empty accumulator. Returns nil
// when neither PrintTree nor SaveTranscript is requested so callers
// can skip the buffering overhead.
func newEventAccumulator(p *Params) *eventAccumulator {
	if !p.PrintTree && p.SaveTranscript == "" {
		return nil
	}
	return &eventAccumulator{
		byID: make(map[string]*toolCallRecord),
	}
}

// observe captures an event into the accumulator. Safe to call on a
// nil receiver — the caller doesn't need to guard.
func (a *eventAccumulator) observe(ev event.Event) {
	if a == nil {
		return
	}
	// Every event goes into the transcript list (for --save-transcript).
	// Strip the permission response channel so json.Encode doesn't
	// choke on `chan PermResponse` — that field is already tagged
	// json:"-" on event.PermReq but we rebuild the envelope for
	// consistency and to avoid accidentally relying on the tag.
	rec := eventRecord{
		Type:       ev.Type,
		Text:       ev.Text,
		ToolCall:   ev.ToolCall,
		ToolResult: ev.ToolResult,
		Error:      ev.Error,
		Usage:      ev.Usage,
		Thinking:   ev.Thinking,
		Info:       ev.Info,
		Timestamp:  time.Now().UTC(),
	}
	if ev.Permission != nil {
		rec.Permission = &permRecord{
			ToolName: ev.Permission.ToolName,
			Pattern:  ev.Permission.Pattern,
		}
	}
	a.events = append(a.events, rec)

	// Tool-tree bookkeeping: pair Start with Result by ID so
	// interleaved tool calls (parallel read tools etc.) don't
	// mis-attribute results.
	switch ev.Type {
	case event.ToolStart:
		if ev.ToolCall == nil {
			return
		}
		// If a prior ToolResult synthesized a record for this ID
		// (out-of-order arrival under backpressure), upgrade it
		// in place instead of creating a duplicate entry that
		// would leave the first one orphaned with no elapsed time.
		// CC Phase 12 review caught this race.
		if existing, ok := a.byID[ev.ToolCall.ID]; ok {
			existing.StartAt = time.Now()
			if len(ev.ToolCall.Input) > 0 {
				existing.Input = ev.ToolCall.Input
			}
			return
		}
		tc := &toolCallRecord{
			ID:      ev.ToolCall.ID,
			Name:    ev.ToolCall.Name,
			Input:   ev.ToolCall.Input,
			StartAt: time.Now(),
		}
		a.tools = append(a.tools, tc)
		a.byID[ev.ToolCall.ID] = tc
	case event.ToolResultEvent:
		if ev.ToolCall == nil || ev.ToolResult == nil {
			return
		}
		tc, ok := a.byID[ev.ToolCall.ID]
		if !ok {
			// Engine re-sends Input on ToolResult; if we missed
			// the Start (e.g. events dropped under backpressure),
			// synthesize an entry from what's available here.
			tc = &toolCallRecord{
				ID:    ev.ToolCall.ID,
				Name:  ev.ToolCall.Name,
				Input: ev.ToolCall.Input,
			}
			a.tools = append(a.tools, tc)
			a.byID[ev.ToolCall.ID] = tc
		}
		if len(ev.ToolCall.Input) > 0 {
			tc.Input = ev.ToolCall.Input
		}
		tc.Output = ev.ToolResult.Output
		tc.Error = ev.ToolResult.Error
		if !tc.StartAt.IsZero() {
			tc.Elapsed = time.Since(tc.StartAt)
		}
	}
}

// renderTree writes a human-readable ASCII tool tree to w. Flat
// rendering — every tool call is a top-level entry regardless of
// subagent nesting. Subagent-spawned tools are distinguishable via
// the `[role]` prefix Phase 12.1 will add once agent/spawn emits
// role metadata on ToolCall.
func (a *eventAccumulator) renderTree(w io.Writer) {
	if a == nil || len(a.tools) == 0 {
		fmt.Fprintln(w, "[tool tree] (no tools called)")
		return
	}
	fmt.Fprintln(w, "[tool tree]")
	for i, tc := range a.tools {
		// ASCII tree glyphs: last item uses `└─`, rest use `├─`.
		glyph := "├─"
		if i == len(a.tools)-1 {
			glyph = "└─"
		}
		status := "✓"
		if tc.Error != "" {
			status = "✗"
		}
		// Truncate input + output for one-line display.
		inputSummary := summarizeToolInput(tc.Name, tc.Input)
		fmt.Fprintf(w, "  %s %s %s%s",
			glyph, status, tc.Name, inputSummary)
		if tc.Elapsed > 0 {
			fmt.Fprintf(w, " (%s)", shortDuration(tc.Elapsed))
		}
		fmt.Fprintln(w)
		if tc.Error != "" {
			fmt.Fprintf(w, "     error: %s\n", truncatePrompt(tc.Error, 80))
		}
	}
}

// writeTranscript dumps every observed event to path as a JSONL
// file. One event per line, ordered as observed. Used by
// --save-transcript (Phase 7 feature that waited for Phase 12's
// accumulator).
func (a *eventAccumulator) writeTranscript(path string) error {
	if a == nil {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, rec := range a.events {
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("encode transcript: %w", err)
		}
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// summarizeToolInput returns a short one-line summary of a tool's
// input for display in the tool tree. Picks tool-specific fields
// so the output is meaningful instead of raw JSON.
func summarizeToolInput(name string, input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var raw map[string]interface{}
	if json.Unmarshal(input, &raw) != nil {
		return " " + truncatePrompt(string(input), 60)
	}
	var parts []string
	// Common input fields across altcode tools. Ordered so the
	// most discriminating field for each tool surfaces first.
	// CC Phase 12 review noted subagent (Task) was rendering as
	// bare `Task` with no hint — `prompt` is the key field.
	if fp, ok := raw["file_path"].(string); ok {
		parts = append(parts, fp)
	}
	if cmd, ok := raw["command"].(string); ok {
		parts = append(parts, "$ "+cmd)
	}
	if pat, ok := raw["pattern"].(string); ok {
		parts = append(parts, "/"+pat+"/")
	}
	if url, ok := raw["url"].(string); ok {
		parts = append(parts, url)
	}
	if q, ok := raw["query"].(string); ok {
		parts = append(parts, q)
	}
	// Subagent spawn (Agent tool): `prompt` is the instruction
	// the subagent was given. `description` is its short label.
	if prompt, ok := raw["prompt"].(string); ok {
		parts = append(parts, "→ "+prompt)
	}
	if desc, ok := raw["description"].(string); ok && len(parts) == 0 {
		// Only surface description if we don't have something more
		// specific — it's usually short and less informative than
		// prompt/command.
		parts = append(parts, desc)
	}
	// Edit tool's old/new strings — show the old text as a hint
	// of what's being replaced.
	if old, ok := raw["old_string"].(string); ok {
		parts = append(parts, "~ "+old)
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + truncatePrompt(strings.Join(parts, " "), 60)
}

// shortDuration formats a duration as "123ms", "1.2s", or "1m2s".
// Phase 12 uses it for per-tool elapsed display; keeps the tree
// rendering scannable without sacrificing accuracy at the
// sub-second scale where most tool calls live.
func shortDuration(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%ds", m, s)
}
