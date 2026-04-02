//go:build !windows

package internal_test

// Practical coding benchmark tasks for altcode.
// Tests real-world coding scenarios across multiple models.
// Inspired by SWE-bench, Aider benchmark, and HumanEval patterns.
//
// Categories:
// 1. Bug fixing (find and fix a bug in code)
// 2. Code generation (write a function from spec)
// 3. Refactoring (improve existing code)
// 4. Multi-file understanding (read multiple files, answer questions)
// 5. Tool orchestration (use multiple tools in sequence)
// 6. Code review (find issues in a diff)

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
)

func benchCfg(t *testing.T) *config.Config {
	t.Helper()
	// Try OpenRouter first
	key := os.Getenv("OPENROUTER")
	if key == "" {
		if data, err := os.ReadFile("../../.env"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "OPENROUTER=") {
					key = strings.TrimPrefix(line, "OPENROUTER=")
				}
			}
		}
	}
	if key == "" {
		// Fall back to Codex relay
		key = os.Getenv("OPENAI_API_KEY")
		if key != "" {
			base := os.Getenv("OPENAI_BASE_URL")
			cfg := config.Default()
			cfg.Model = "openai/gpt-5.4"
			cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: base}
			return cfg
		}
		t.Skip("No API key available")
	}
	cfg := config.Default()
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: "https://openrouter.ai/api"}
	return cfg
}

type benchModel struct {
	name  string
	model string
}

func benchModels() []benchModel {
	return []benchModel{
		{"DeepSeek", "openai/deepseek/deepseek-chat-v3-0324"},
		{"Qwen-Coder", "openai/qwen/qwen3-coder-next"},
		{"Kimi", "openai/moonshotai/kimi-k2.5"},
	}
}

func benchRun(t *testing.T, cfg *config.Config, model, prompt string, timeout time.Duration) (string, []event.Event) {
	t.Helper()
	cfgCopy := *cfg
	cfgCopy.Model = model
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	eng, err := engine.New(engine.EngineParams{Config: &cfgCopy})
	if err != nil {
		t.Fatalf("Engine: %v", err)
	}

	var events []event.Event
	var text string
	for ev := range eng.Run(ctx, prompt) {
		events = append(events, ev)
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
	}
	return text, events
}

func benchExec(t *testing.T, cfg *config.Config, model, prompt string, timeout time.Duration) string {
	t.Helper()
	cfgCopy := *cfg
	cfgCopy.Model = model
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var buf bytes.Buffer
	exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{Config: &cfgCopy},
		Prompt:       prompt,
		Writer:       &buf,
	})
	return buf.String()
}

func toolCallCount(events []event.Event) int {
	n := 0
	for _, ev := range events {
		if ev.Type == event.ToolResultEvent {
			n++
		}
	}
	return n
}

// =============================================================================
// BENCHMARK 1: Bug Fixing — find the bug in a function
// (Inspired by SWE-bench: given a buggy function, identify and fix it)
// =============================================================================

func TestBench_BugFix(t *testing.T) {
	t.Parallel()
	cfg := benchCfg(t)

	prompt := `Here is a buggy Go function:

` + "```go" + `
func fibonacci(n int) int {
    if n <= 0 {
        return 0
    }
    if n == 1 {
        return 1
    }
    a, b := 0, 1
    for i := 2; i < n; i++ {  // BUG: should be i <= n
        a, b = b, a+b
    }
    return b
}
` + "```" + `

The function should return fibonacci(5) = 5 but returns 3.
What is the bug and what is the fix? Be specific about the line.`

	for _, m := range benchModels() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text := benchExec(t, cfg, m.model, prompt, 30*time.Second)
			t.Logf("[%s] %.300s", m.name, text)

			score := 0
			lower := strings.ToLower(text)
			if strings.Contains(lower, "i < n") || strings.Contains(lower, "i <= n") || strings.Contains(lower, "off by one") || strings.Contains(lower, "off-by-one") {
				score++
			}
			if strings.Contains(lower, "<=") || strings.Contains(lower, "less than or equal") {
				score++
			}
			t.Logf("[%s] Bug fix score: %d/2", m.name, score)
		})
	}
}

// =============================================================================
// BENCHMARK 2: Code Generation — write a function from spec
// (Inspired by HumanEval: implement a function given description + examples)
// =============================================================================

func TestBench_CodeGen(t *testing.T) {
	t.Parallel()
	cfg := benchCfg(t)

	prompt := `Write a Go function called 'isPalindrome' that checks if a string is a palindrome.
It should:
- Ignore case (so "Racecar" is a palindrome)
- Ignore non-alphanumeric characters (so "A man, a plan, a canal: Panama" is a palindrome)
- Return a bool

Show ONLY the function, no explanation.`

	for _, m := range benchModels() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text := benchExec(t, cfg, m.model, prompt, 30*time.Second)
			t.Logf("[%s] %.400s", m.name, text)

			score := 0
			if strings.Contains(text, "func isPalindrome") || strings.Contains(text, "func IsPalindrome") {
				score++
			}
			if strings.Contains(text, "strings.ToLower") || strings.Contains(text, "unicode.ToLower") || strings.Contains(text, "toLower") {
				score++
			}
			if strings.Contains(text, "IsLetter") || strings.Contains(text, "IsDigit") || strings.Contains(text, "alphanumeric") || strings.Contains(text, "isalnum") {
				score++
			}
			t.Logf("[%s] Code gen score: %d/3", m.name, score)
		})
	}
}

// =============================================================================
// BENCHMARK 3: Tool Orchestration — use multiple tools to complete a task
// (Unique to coding agents: requires reading, searching, and answering)
// =============================================================================

func TestBench_ToolOrchestration(t *testing.T) {
	t.Parallel()
	cfg := benchCfg(t)

	prompt := `Use tools to answer: How many Go source files (*.go, not test files) are in the internal/engine/ directory?
Use ls or glob to find them. Reply with just the number.`

	for _, m := range benchModels() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text, events := benchRun(t, cfg, m.model, prompt, 45*time.Second)
			tools := toolCallCount(events)
			t.Logf("[%s] Tools used: %d, Answer: %.100s", m.name, tools, text)

			score := 0
			if tools > 0 {
				score++ // used at least one tool
			}
			if strings.Contains(text, "2") || strings.Contains(text, "two") {
				score++ // correct answer (engine.go + collector.go)
			}
			t.Logf("[%s] Orchestration score: %d/2", m.name, score)
		})
	}
}

// =============================================================================
// BENCHMARK 4: Multi-file Understanding — read and synthesize
// =============================================================================

func TestBench_MultiFileUnderstanding(t *testing.T) {
	t.Parallel()
	cfg := benchCfg(t)

	prompt := `Use the read tool to read both internal/provider/message.go and internal/event/event.go.
Then answer: What is the difference between provider.Message and event.Event?
Which one is used for communication with the AI model API, and which for internal engine-to-TUI communication?
Be brief (2-3 sentences).`

	for _, m := range benchModels() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text, events := benchRun(t, cfg, m.model, prompt, 60*time.Second)
			tools := toolCallCount(events)
			t.Logf("[%s] Tools: %d, Answer: %.300s", m.name, tools, text)

			score := 0
			lower := strings.ToLower(text)
			if tools >= 2 {
				score++ // read both files
			}
			if strings.Contains(lower, "api") || strings.Contains(lower, "provider") || strings.Contains(lower, "model") {
				score++
			}
			if strings.Contains(lower, "tui") || strings.Contains(lower, "engine") || strings.Contains(lower, "internal") {
				score++
			}
			t.Logf("[%s] Understanding score: %d/3", m.name, score)
		})
	}
}

// =============================================================================
// BENCHMARK 5: File Creation — create a new file with correct content
// =============================================================================

func TestBench_FileCreation(t *testing.T) {
	t.Parallel()
	cfg := benchCfg(t)
	dir := t.TempDir()

	prompt := `Use the write tool to create a file at ` + filepath.Join(dir, "hello.go") + ` with this content:
package main

import "fmt"

func main() {
    fmt.Println("Hello from altcode benchmark!")
}

Then use the bash tool to run: go run ` + filepath.Join(dir, "hello.go") + `
Show the output.`

	for _, m := range benchModels() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text, events := benchRun(t, cfg, m.model, prompt, 60*time.Second)
			tools := toolCallCount(events)
			t.Logf("[%s] Tools: %d, Output: %.200s", m.name, tools, text)

			score := 0
			if tools >= 2 {
				score++ // write + bash
			}
			if strings.Contains(text, "Hello from altcode") {
				score++ // program ran successfully
			}
			// Check file was actually created
			if _, err := os.Stat(filepath.Join(dir, "hello.go")); err == nil {
				score++
			}
			t.Logf("[%s] File creation score: %d/3", m.name, score)
		})
	}
}

// =============================================================================
// BENCHMARK 6: Grep + Analyze — search codebase and answer
// =============================================================================

func TestBench_GrepAnalyze(t *testing.T) {
	t.Parallel()
	cfg := benchCfg(t)

	prompt := `Use grep to find all files that contain "func New" in the internal/ directory.
How many different packages define a New function? List them.`

	for _, m := range benchModels() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text, events := benchRun(t, cfg, m.model, prompt, 45*time.Second)
			tools := toolCallCount(events)
			t.Logf("[%s] Tools: %d, Answer: %.300s", m.name, tools, text)

			score := 0
			if tools > 0 {
				score++
			}
			lower := strings.ToLower(text)
			// Should find engine, hooks, memory, mcp, permission, etc.
			found := 0
			for _, pkg := range []string{"engine", "hooks", "memory", "permission", "store", "mcp"} {
				if strings.Contains(lower, pkg) {
					found++
				}
			}
			if found >= 3 {
				score++
			}
			t.Logf("[%s] Grep score: %d/2 (found %d packages)", m.name, score, found)
		})
	}
}

// =============================================================================
// BENCHMARK 7: JSON Mode — structured output
// =============================================================================

func TestBench_JSONOutput(t *testing.T) {
	t.Parallel()
	cfg := benchCfg(t)

	for _, m := range benchModels() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			cfgCopy := *cfg
			cfgCopy.Model = m.model
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var buf bytes.Buffer
			exec.Run(ctx, exec.Params{
				EngineParams: engine.EngineParams{Config: &cfgCopy},
				Prompt:       "Say hello",
				JSON:         true,
				Writer:       &buf,
			})

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			validJSON := 0
			hasTextDelta := false
			hasDone := false
			for _, line := range lines {
				var ev event.Event
				if json.Unmarshal([]byte(line), &ev) == nil {
					validJSON++
					if ev.Type == event.TextDelta {
						hasTextDelta = true
					}
					if ev.Type == event.Done {
						hasDone = true
					}
				}
			}
			t.Logf("[%s] JSONL lines: %d, valid: %d, textDelta: %v, done: %v",
				m.name, len(lines), validJSON, hasTextDelta, hasDone)

			score := 0
			if validJSON > 0 {
				score++
			}
			if hasTextDelta {
				score++
			}
			if hasDone {
				score++
			}
			t.Logf("[%s] JSON score: %d/3", m.name, score)
		})
	}
}
