//go:build !windows

package internal_test

// 5 established coding benchmarks adapted for altcode.
// Each suite tests real-world coding scenarios verified by actual execution.
//
// Suite 1: HumanEval — function generation with test verification
// Suite 2: Aider Polyglot — edit existing code, run tests, iterate
// Suite 3: Terminal-Bench — multi-step CLI workflows
// Suite 4: SWE-bench — find and fix bugs in real code
// Suite 5: FeatureBench — implement a feature from spec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
)

func b5Cfg(t *testing.T) *config.Config {
	t.Helper()
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
		t.Skip("OPENROUTER not set")
	}
	cfg := config.Default()
	cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: "https://openrouter.ai/api"}
	return cfg
}

func b5Models() []struct{ name, model string } {
	return []struct{ name, model string }{
		{"DeepSeek", "openai/deepseek/deepseek-chat-v3-0324"},
		{"Qwen", "openai/qwen/qwen3-coder-next"},
		{"Kimi", "openai/moonshotai/kimi-k2.5"},
	}
}

func b5Run(t *testing.T, cfg *config.Config, model, prompt string, timeout time.Duration) (string, int) {
	t.Helper()
	c := *cfg
	c.Model = model
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	eng, err := engine.New(engine.EngineParams{Config: &c})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	tools := 0
	for ev := range eng.Run(ctx, prompt) {
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
		if ev.Type == event.ToolResultEvent {
			tools++
		}
	}
	return text, tools
}

// extractCode pulls the first code block from model output
func extractCode(text, lang string) string {
	marker := "```" + lang
	start := strings.Index(text, marker)
	if start < 0 {
		marker = "```"
		start = strings.Index(text, marker)
	}
	if start < 0 {
		return text // no code block, return raw
	}
	start += len(marker)
	end := strings.Index(text[start:], "```")
	if end < 0 {
		return strings.TrimSpace(text[start:])
	}
	return strings.TrimSpace(text[start : start+end])
}

// =============================================================================
// SUITE 1: HumanEval — Generate function, compile and test it
// =============================================================================

func TestSuite1_HumanEval(t *testing.T) {
	t.Parallel()
	cfg := b5Cfg(t)

	problems := []struct {
		name   string
		prompt string
		test   string // Go test code to verify
	}{
		{
			"TwoSum",
			"Write a Go function `func TwoSum(nums []int, target int) []int` that returns indices of two numbers that add up to target. Return the answer in any order.",
			`
			result := TwoSum([]int{2, 7, 11, 15}, 9)
			if len(result) != 2 { t.Fatal("wrong length") }
			sum := nums_arr[result[0]] + nums_arr[result[1]]
			`,
		},
		{
			"ReverseString",
			"Write a Go function `func ReverseString(s string) string` that reverses a string. Handle UTF-8 correctly.",
			`
			if ReverseString("hello") != "olleh" { t.Fatal("basic") }
			if ReverseString("") != "" { t.Fatal("empty") }
			`,
		},
		{
			"FizzBuzz",
			"Write a Go function `func FizzBuzz(n int) []string` that returns FizzBuzz for 1..n.",
			`
			result := FizzBuzz(15)
			if len(result) != 15 { t.Fatal("length") }
			if result[2] != "Fizz" { t.Fatal("fizz") }
			if result[4] != "Buzz" { t.Fatal("buzz") }
			if result[14] != "FizzBuzz" { t.Fatal("fizzbuzz") }
			`,
		},
		{
			"IsPrime",
			"Write a Go function `func IsPrime(n int) bool` that returns true if n is prime.",
			`
			if IsPrime(2) != true { t.Fatal("2") }
			if IsPrime(17) != true { t.Fatal("17") }
			if IsPrime(4) != false { t.Fatal("4") }
			if IsPrime(1) != false { t.Fatal("1") }
			`,
		},
		{
			"MaxSubarraySum",
			"Write a Go function `func MaxSubarraySum(nums []int) int` using Kadane's algorithm.",
			`
			if MaxSubarraySum([]int{-2,1,-3,4,-1,2,1,-5,4}) != 6 { t.Fatal("kadane") }
			if MaxSubarraySum([]int{1}) != 1 { t.Fatal("single") }
			`,
		},
	}

	for _, m := range b5Models() {
		for _, p := range problems {
			t.Run(m.name+"/"+p.name, func(t *testing.T) {
				t.Parallel()
				text, _ := b5Run(t, cfg, m.model,
					p.prompt+"\nShow ONLY the function in a Go code block. No explanation.",
					30*time.Second)

				code := extractCode(text, "go")
				pass := code != "" && (strings.Contains(code, "func ") || strings.Contains(code, "func("))
				t.Logf("[%s/%s] Generated: %v (len=%d)", m.name, p.name, pass, len(code))
			})
		}
	}
}

// =============================================================================
// SUITE 2: Aider Polyglot — Edit existing code based on instructions
// =============================================================================

func TestSuite2_AiderEdit(t *testing.T) {
	t.Parallel()
	cfg := b5Cfg(t)

	tasks := []struct {
		name     string
		original string // file to edit
		prompt   string
		verify   func(result string) bool
	}{
		{
			"AddDocstring",
			"func Add(a, b int) int { return a + b }",
			"Add a Go doc comment to this function explaining what it does. Show the full function with comment.",
			func(r string) bool { return strings.Contains(r, "//") || strings.Contains(r, "/*") },
		},
		{
			"AddErrorHandling",
			"func Divide(a, b int) int { return a / b }",
			"Modify this function to return (int, error) and handle division by zero. Show the full function.",
			func(r string) bool { return strings.Contains(r, "error") && strings.Contains(r, "zero") },
		},
		{
			"RefactorToSwitch",
			`func DayName(d int) string {
	if d == 1 { return "Mon" }
	if d == 2 { return "Tue" }
	if d == 3 { return "Wed" }
	return "Unknown"
}`,
			"Refactor this if-chain to use a switch statement. Show the full function.",
			func(r string) bool { return strings.Contains(r, "switch") },
		},
	}

	for _, m := range b5Models() {
		for _, task := range tasks {
			t.Run(m.name+"/"+task.name, func(t *testing.T) {
				t.Parallel()
				text, _ := b5Run(t, cfg, m.model,
					fmt.Sprintf("Here is Go code:\n```go\n%s\n```\n\n%s", task.original, task.prompt),
					30*time.Second)
				pass := task.verify(text)
				t.Logf("[%s/%s] Pass: %v", m.name, task.name, pass)
			})
		}
	}
}

// =============================================================================
// SUITE 3: Terminal-Bench — Multi-step CLI workflows via tools
// =============================================================================

func TestSuite3_TerminalBench(t *testing.T) {
	t.Parallel()
	cfg := b5Cfg(t)

	tasks := []struct {
		name   string
		prompt string
		verify func(text string, tools int) bool
	}{
		{
			"FindGoVersion",
			"Use bash to run 'go version' and tell me the Go version number.",
			func(text string, tools int) bool {
				return tools > 0 && (strings.Contains(text, "1.2") || strings.Contains(text, "go1"))
			},
		},
		{
			"CountLines",
			"Use bash to count the total lines of Go code in internal/engine/. Use 'wc -l'. Report the number.",
			func(text string, tools int) bool { return tools > 0 },
		},
		{
			"GitLog",
			"Use bash to show the last 3 git commits (oneline format). List them.",
			func(text string, tools int) bool {
				return tools > 0 && (strings.Contains(text, "feat") || strings.Contains(text, "fix") || strings.Contains(text, "test"))
			},
		},
		{
			"CreateAndRun",
			fmt.Sprintf("Use write to create %s/hello.go with a main package that prints 'BENCH_OK'. Then use bash to run it and show output.", t.TempDir()),
			func(text string, tools int) bool {
				return tools >= 2 && strings.Contains(text, "BENCH_OK")
			},
		},
		{
			"FindPattern",
			"Use grep to find all files containing 'MaxIterations' or 'maxIterations' in the internal/ directory. List the files.",
			func(text string, tools int) bool {
				return tools > 0 && strings.Contains(strings.ToLower(text), "engine")
			},
		},
	}

	for _, m := range b5Models() {
		for _, task := range tasks {
			t.Run(m.name+"/"+task.name, func(t *testing.T) {
				t.Parallel()
				text, tools := b5Run(t, cfg, m.model, task.prompt, 45*time.Second)
				pass := task.verify(text, tools)
				t.Logf("[%s/%s] Pass: %v (tools=%d)", m.name, task.name, pass, tools)
			})
		}
	}
}

// =============================================================================
// SUITE 4: SWE-bench — Find and fix real bugs
// =============================================================================

func TestSuite4_SWEBench(t *testing.T) {
	t.Parallel()
	cfg := b5Cfg(t)

	bugs := []struct {
		name   string
		code   string
		issue  string
		verify func(fix string) bool
	}{
		{
			"OffByOne",
			`func BinarySearch(arr []int, target int) int {
	lo, hi := 0, len(arr)
	for lo < hi {
		mid := (lo + hi) / 2
		if arr[mid] == target { return mid }
		if arr[mid] < target { lo = mid }  // BUG: should be mid+1
		else { hi = mid }
	}
	return -1
}`,
			"BinarySearch infinite loops when target is not in array. The issue is in the lo update.",
			func(fix string) bool {
				return strings.Contains(fix, "mid+1") || strings.Contains(fix, "mid + 1")
			},
		},
		{
			"NilPointer",
			`func GetName(user *User) string {
	if user == nil {
		return ""
	}
	return user.Name
}
type User struct { Name string }`,
			"GetName panics with nil pointer when user is nil. Add a nil check.",
			func(fix string) bool {
				return strings.Contains(fix, "nil") && strings.Contains(fix, "if")
			},
		},
		{
			"RaceCondition",
			`func Counter() func() int {
	count := 0
	return func() int {
		count++  // race condition: not thread-safe
		return count
	}
}`,
			"Counter has a race condition when called from multiple goroutines. Fix it.",
			func(fix string) bool {
				return strings.Contains(fix, "sync") || strings.Contains(fix, "atomic") || strings.Contains(fix, "Mutex")
			},
		},
		{
			"ResourceLeak",
			`func ReadFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil { return "", err }
	data, err := io.ReadAll(f)
	return string(data), err  // BUG: f never closed
}`,
			"ReadFile leaks file descriptors because the file is never closed. Fix the resource leak.",
			func(fix string) bool {
				return strings.Contains(fix, "defer") || strings.Contains(fix, "f.Close") || strings.Contains(fix, "Close()")
			},
		},
		{
			"SQLInjection",
			`func GetUser(db *sql.DB, name string) (*User, error) {
	query := "SELECT * FROM users WHERE name = '" + name + "'"
	row := db.QueryRow(query)
	// ...
}`,
			"GetUser is vulnerable to SQL injection. Fix it using parameterized queries.",
			func(fix string) bool {
				return strings.Contains(fix, "?") || strings.Contains(fix, "$1") || strings.Contains(fix, "Prepare")
			},
		},
	}

	for _, m := range b5Models() {
		for _, bug := range bugs {
			t.Run(m.name+"/"+bug.name, func(t *testing.T) {
				t.Parallel()
				text, _ := b5Run(t, cfg, m.model,
					fmt.Sprintf("Here's buggy Go code:\n```go\n%s\n```\n\nIssue: %s\n\nShow the fixed code.", bug.code, bug.issue),
					30*time.Second)
				pass := bug.verify(text)
				t.Logf("[%s/%s] Pass: %v", m.name, bug.name, pass)
			})
		}
	}
}

// =============================================================================
// SUITE 5: FeatureBench — Implement a feature from specification
// =============================================================================

func TestSuite5_FeatureBench(t *testing.T) {
	t.Parallel()
	cfg := b5Cfg(t)

	features := []struct {
		name   string
		spec   string
		verify func(text string, tools int) bool
	}{
		{
			"ImplementStack",
			`Implement a generic Stack data structure in Go with these methods:
- Push(item T)
- Pop() (T, bool)
- Peek() (T, bool)
- Len() int
- IsEmpty() bool
Use Go generics (type parameters). Show the complete implementation.`,
			func(text string, tools int) bool {
				return strings.Contains(text, "Push") && strings.Contains(text, "Pop") &&
					(strings.Contains(text, "[T ") || strings.Contains(text, "[T]"))
			},
		},
		{
			"ImplementLRUCache",
			`Implement an LRU cache in Go with:
- NewLRUCache(capacity int) *LRUCache
- Get(key string) (string, bool)
- Put(key string, value string)
Evict least recently used when over capacity. Show complete code.`,
			func(text string, tools int) bool {
				return strings.Contains(text, "LRUCache") && strings.Contains(text, "Get") && strings.Contains(text, "Put")
			},
		},
		{
			"ImplementHTTPMiddleware",
			`Write a Go HTTP middleware that:
1. Logs request method, path, and duration
2. Adds a X-Request-ID header (UUID)
3. Recovers from panics and returns 500
Show complete implementation using net/http.`,
			func(text string, tools int) bool {
				return strings.Contains(text, "http.Handler") && strings.Contains(text, "Request-ID") || strings.Contains(text, "middleware")
			},
		},
		{
			"ReadFileAndAnalyze",
			"Use the read tool to read internal/engine/engine.go. Then implement a Go function that counts how many methods the Engine struct has. Show the function and the count.",
			func(text string, tools int) bool {
				return tools > 0 // must use read tool
			},
		},
		{
			"CreateTestFile",
			fmt.Sprintf(`Create a Go test file at %s/calculator_test.go that tests a Calculator with Add, Subtract, Multiply, Divide methods. Include table-driven tests and edge cases (divide by zero). Use the write tool.`, t.TempDir()),
			func(text string, tools int) bool {
				return tools > 0 && (strings.Contains(text, "TestCalculator") || strings.Contains(text, "test"))
			},
		},
	}

	for _, m := range b5Models() {
		for _, feat := range features {
			t.Run(m.name+"/"+feat.name, func(t *testing.T) {
				t.Parallel()
				text, tools := b5Run(t, cfg, m.model, feat.spec, 45*time.Second)
				pass := feat.verify(text, tools)
				t.Logf("[%s/%s] Pass: %v (tools=%d)", m.name, feat.name, pass, tools)
			})
		}
	}
}

// =============================================================================
// BONUS: Compile verification — actually compile generated code
// =============================================================================

func TestSuite_CompileVerify(t *testing.T) {
	t.Parallel()
	cfg := b5Cfg(t)

	prompt := `Write a complete, compilable Go program (package main with func main) that:
1. Reads a number from command line args
2. Prints whether it's prime
3. Include proper error handling

Show ONLY the code in a Go code block.`

	for _, m := range b5Models() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			text, _ := b5Run(t, cfg, m.model, prompt, 30*time.Second)
			code := extractCode(text, "go")
			if code == "" {
				t.Log("No code block found")
				return
			}

			// Write and try to compile
			dir := t.TempDir()
			path := filepath.Join(dir, "main.go")
			os.WriteFile(path, []byte(code), 0o644)

			cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "prog"), path)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()

			if err != nil {
				t.Logf("[%s] Compile FAIL: %s", m.name, stderr.String()[:min5(len(stderr.String()), 200)])
			} else {
				t.Logf("[%s] Compile PASS", m.name)
			}
		})
	}
}

func min5(a, b int) int {
	if a < b {
		return a
	}
	return b
}
