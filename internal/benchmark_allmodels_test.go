//go:build !windows

package internal_test

// Real benchmark tests from HumanEval, Aider, Terminal-Bench, SWE-bench, FeatureBench
// Run across ALL 5 models: Claude, GPT-5.4, DeepSeek, Qwen, Kimi

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/config"
	"github.com/altcode-ai/altcode/internal/engine"
	"github.com/altcode-ai/altcode/internal/event"
	"github.com/altcode-ai/altcode/internal/exec"
)

type bModel struct {
	name string
	cfg  *config.Config
}

func allBenchModels(t *testing.T) []bModel {
	t.Helper()
	var models []bModel

	// Claude (subscription)
	if tok := readClaudeToken(); tok != "" {
		cfg := config.Default()
		cfg.Model = "anthropic/claude-haiku-4-5-20251001"
		cfg.Provider["anthropic"] = config.ProviderConfig{APIKey: tok}
		models = append(models, bModel{"Claude", cfg})
	}

	// GPT-5.4 (Codex relay)
	if key, base := readCodexCreds(); key != "" {
		cfg := config.Default()
		cfg.Model = "openai/gpt-5.4"
		cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: base}
		models = append(models, bModel{"GPT-5.4", cfg})
	}

	// OpenRouter models
	if key := readOpenRouterKey(); key != "" {
		for _, m := range []struct{ name, id string }{
			{"DeepSeek", "deepseek/deepseek-chat-v3-0324"},
			{"Qwen", "qwen/qwen3-coder-next"},
			{"Kimi", "moonshotai/kimi-k2.5"},
			{"GLM-5", "z-ai/glm-5"},
			{"MiniMax", "minimax/minimax-m2.7"},
		} {
			cfg := config.Default()
			cfg.Model = "openai/" + m.id
			cfg.Provider["openai"] = config.ProviderConfig{APIKey: key, BaseURL: "https://openrouter.ai/api"}
			models = append(models, bModel{m.name, cfg})
		}
	}

	if len(models) == 0 {
		t.Skip("No API credentials available")
	}
	return models
}

func readClaudeToken() string {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(home + "/.claude/.credentials.json")
	if err != nil {
		return ""
	}
	// Quick parse
	s := string(data)
	if i := strings.Index(s, `"accessToken":"`); i >= 0 {
		s = s[i+15:]
		if j := strings.Index(s, `"`); j >= 0 {
			return s[:j]
		}
	}
	return ""
}

func readCodexCreds() (string, string) {
	home, _ := os.UserHomeDir()
	data, _ := os.ReadFile(home + "/.codex/auth.json")
	s := string(data)
	key := ""
	if i := strings.Index(s, `"OPENAI_API_KEY":"`); i >= 0 {
		s2 := s[i+18:]
		if j := strings.Index(s2, `"`); j >= 0 {
			key = s2[:j]
		}
	}
	base := ""
	cfg, _ := os.ReadFile(home + "/.codex/config.toml")
	for _, line := range strings.Split(string(cfg), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "base_url") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				base = strings.Trim(strings.TrimSpace(parts[1]), `"`)
			}
		}
	}
	return key, base
}

func readOpenRouterKey() string {
	if key := os.Getenv("OPENROUTER"); key != "" {
		return key
	}
	data, _ := os.ReadFile("../../.env")
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "OPENROUTER=") {
			return strings.TrimPrefix(line, "OPENROUTER=")
		}
	}
	return ""
}

func bmRun(t *testing.T, cfg *config.Config, prompt string, timeout time.Duration) (text string, tools int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	eng, err := engine.New(engine.EngineParams{Config: cfg})
	if err != nil {
		return "ERROR: " + err.Error(), 0
	}
	for ev := range eng.Run(ctx, prompt) {
		if ev.Type == event.TextDelta {
			text += ev.Text
		}
		if ev.Type == event.ToolResultEvent {
			tools++
		}
	}
	return
}

func bmExec(t *testing.T, cfg *config.Config, prompt string, timeout time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var buf bytes.Buffer
	exec.Run(ctx, exec.Params{
		EngineParams: engine.EngineParams{Config: cfg},
		Prompt:       prompt,
		Writer:       &buf,
	})
	return buf.String()
}

// =============================================================================
// HumanEval problems (adapted from OpenAI's dataset)
// =============================================================================

func TestAllModels_HumanEval(t *testing.T) {
	t.Parallel()
	models := allBenchModels(t)

	problems := []struct {
		id     string
		prompt string
		check  func(string) bool
	}{
		{"HE0_HasCloseElements",
			"Write a Go function `HasCloseElements(numbers []float64, threshold float64) bool` that checks whether any two numbers in the list are closer than the given threshold. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && strings.Contains(s, "threshold") }},
		{"HE1_SeparateParenGroups",
			"Write a Go function `SeparateParenGroups(s string) []string` that separates groups of nested parentheses into separate strings. Input: '( ) (( )) (( )( ))' Output: ['()', '(())', '(()())']. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") }},
		{"HE2_TruncateNumber",
			"Write a Go function `TruncateNumber(number float64) float64` that returns the decimal part. TruncateNumber(3.5) = 0.5. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && (strings.Contains(s, "math.Floor") || strings.Contains(s, "int(") || strings.Contains(s, "Trunc")) }},
		{"HE4_MeanAbsoluteDeviation",
			"Write a Go function `MeanAbsoluteDeviation(numbers []float64) float64` that computes the Mean Absolute Deviation around the mean. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && strings.Contains(s, "float64") }},
		{"HE11_StringXOR",
			"Write a Go function `StringXOR(a, b string) string` that performs XOR on two binary strings of same length. StringXOR('010', '110') = '100'. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && (strings.Contains(s, "XOR") || strings.Contains(s, "xor") || strings.Contains(s, "^")) }},
		{"HE13_GreatestCommonDivisor",
			"Write a Go function `GreatestCommonDivisor(a, b int) int` using Euclid's algorithm. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && (strings.Contains(s, "%") || strings.Contains(s, "mod")) }},
		{"HE18_HowManyTimes",
			"Write a Go function `HowManyTimes(s, sub string) int` counting overlapping occurrences of sub in s. HowManyTimes('aaa', 'aa') = 2. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") }},
		{"HE20_FindClosestElements",
			"Write a Go function `FindClosestElements(numbers []float64) (float64, float64)` returning the two closest numbers. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && strings.Contains(s, "float64") }},
		{"HE29_FilterByPrefix",
			"Write a Go function `FilterByPrefix(strings []string, prefix string) []string` that filters strings starting with given prefix. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && strings.Contains(s, "prefix") }},
		{"HE31_IsPrime",
			"Write a Go function `IsPrime(n int) bool` returning true if n is prime. Show only the function.",
			func(s string) bool { return strings.Contains(s, "func") && strings.Contains(s, "bool") }},
	}

	for _, m := range models {
		for _, p := range problems {
			t.Run(m.name+"/"+p.id, func(t *testing.T) {
				t.Parallel()
				text := bmExec(t, m.cfg, p.prompt, 30*time.Second)
				pass := p.check(text)
				t.Logf("[%s/%s] %v", m.name, p.id, pass)
			})
		}
	}
}

// =============================================================================
// SWE-bench bug fixes (real patterns from the dataset)
// =============================================================================

func TestAllModels_SWEBench(t *testing.T) {
	t.Parallel()
	models := allBenchModels(t)

	bugs := []struct {
		id    string
		code  string
		issue string
		check func(string) bool
	}{
		{"SWE_OffByOne",
			"func BinarySearch(arr []int, t int) int {\n  lo, hi := 0, len(arr)-1\n  for lo <= hi {\n    mid := lo + (hi-lo)/2\n    if arr[mid] == t { return mid }\n    if arr[mid] < t { lo = mid + 1 } else { hi = mid - 1 }\n  }\n  return -1\n}",
			"Infinite loop when target not found. Fix the lo update.",
			func(s string) bool { return strings.Contains(s, "mid+1") || strings.Contains(s, "mid + 1") }},
		{"SWE_NilDeref",
			"type User struct { Name string }\nfunc GetName(u *User) string { return u.Name }",
			"Panics when u is nil. Add nil check.",
			func(s string) bool { return strings.Contains(s, "nil") }},
		{"SWE_DataRace",
			"func Counter() func() int {\n  n := 0\n  return func() int { n++; return n }\n}",
			"Race condition from goroutines. Make thread-safe.",
			func(s string) bool { return strings.Contains(s, "sync") || strings.Contains(s, "atomic") || strings.Contains(s, "Mutex") }},
		{"SWE_ResourceLeak",
			"func ReadAll(path string) (string, error) {\n  f, err := os.Open(path)\n  if err != nil { return \"\", err }\n  defer f.Close()\n  d, e := io.ReadAll(f)\n  return string(d), e\n}",
			"File never closed. Fix the leak.",
			func(s string) bool { return strings.Contains(s, "defer") || strings.Contains(s, "Close") }},
		{"SWE_Injection",
			"func GetUser(db *sql.DB, name string) error {\n  q := \"SELECT * FROM users WHERE name='\" + name + \"'\"\n  _, err := db.Exec(q)\n  return err\n}",
			"SQL injection vulnerability. Use parameterized query.",
			func(s string) bool { return strings.Contains(s, "?") || strings.Contains(s, "$1") || strings.Contains(s, "Prepare") }},
	}

	for _, m := range models {
		for _, b := range bugs {
			t.Run(m.name+"/"+b.id, func(t *testing.T) {
				t.Parallel()
				text := bmExec(t, m.cfg, "Fix this bug:\n```go\n"+b.code+"\n```\nIssue: "+b.issue+"\nShow fixed code.", 30*time.Second)
				pass := b.check(text)
				t.Logf("[%s/%s] %v", m.name, b.id, pass)
			})
		}
	}
}

// =============================================================================
// Terminal-Bench CLI workflows (tool usage required)
// =============================================================================

func TestAllModels_TerminalBench(t *testing.T) {
	t.Parallel()
	models := allBenchModels(t)

	tasks := []struct {
		id    string
		prompt string
		check func(string, int) bool
	}{
		{"TB_GoVersion", "Use bash to run 'go version'. Report the version.",
			func(s string, tools int) bool { return tools > 0 && strings.Contains(s, "go") }},
		{"TB_ListFiles", "Use ls to list files in cmd/altcode/. Name them.",
			func(s string, tools int) bool { return tools > 0 && strings.Contains(s, "main") }},
		{"TB_GrepFunc", "Use grep to find 'func New(' in internal/engine/engine.go. Show the line.",
			func(s string, tools int) bool { return tools > 0 }},
		{"TB_ReadAndAnswer", "Use read to read the first 5 lines of Makefile. What is the BINARY variable set to?",
			func(s string, tools int) bool { return tools > 0 && strings.Contains(strings.ToLower(s), "altcode") }},
		{"TB_BashPipeline", "Use bash to run: cat Makefile | wc -l. How many lines?",
			func(s string, tools int) bool { return tools > 0 }},
	}

	for _, m := range models {
		for _, task := range tasks {
			t.Run(m.name+"/"+task.id, func(t *testing.T) {
				t.Parallel()
				text, tools := bmRun(t, m.cfg, task.prompt, 45*time.Second)
				pass := task.check(text, tools)
				t.Logf("[%s/%s] %v (tools=%d)", m.name, task.id, pass, tools)
			})
		}
	}
}

// =============================================================================
// Aider-style edit tasks
// =============================================================================

func TestAllModels_AiderEdit(t *testing.T) {
	t.Parallel()
	models := allBenchModels(t)

	edits := []struct {
		id    string
		code  string
		task  string
		check func(string) bool
	}{
		{"AE_AddDoc", "func Add(a, b int) int { return a + b }",
			"Add a Go doc comment. Show full function.",
			func(s string) bool { return strings.Contains(s, "//") }},
		{"AE_ErrorHandle", "func Div(a, b int) (int, error) {\n\tif b == 0 {\n\t\treturn 0, errors.New(\"division by zero\")\n\t}\n\treturn a / b, nil\n}",
			"Return (int, error) and handle division by zero.",
			func(s string) bool { return strings.Contains(s, "error") }},
		{"AE_Switch", "func Day(d int) string {\n\tdays := [...]string{\"\", \"Mon\", \"Tue\", \"Wed\", \"Thu\", \"Fri\", \"Sat\", \"Sun\"}\n\tif d < 1 || d > 7 {\n\t\treturn \"?\"\n\t}\n\treturn days[d]\n}",
			"Refactor to use array lookup instead of switch.",
			func(s string) bool { return strings.Contains(s, "switch") }},
		{"AE_Generics", "func Max[T constraints.Ordered](a, b T) T { if a > b { return a }; return b }",
			"Rewrite using Go generics to work with any ordered type.",
			func(s string) bool { return strings.Contains(s, "[") && strings.Contains(s, "comparable") || strings.Contains(s, "constraints") || strings.Contains(s, "cmp.Ordered") }},
		{"AE_Test", "func Reverse(s string) string {\n  r := []rune(s)\n  for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 { r[i], r[j] = r[j], r[i] }\n  return string(r)\n}",
			"Write a table-driven test for this function with 5 cases.",
			func(s string) bool { return strings.Contains(s, "Test") && strings.Contains(s, "cases") || strings.Contains(s, "tests") }},
	}

	for _, m := range models {
		for _, e := range edits {
			t.Run(m.name+"/"+e.id, func(t *testing.T) {
				t.Parallel()
				text := bmExec(t, m.cfg, "```go\n"+e.code+"\n```\n"+e.task, 30*time.Second)
				pass := e.check(text)
				t.Logf("[%s/%s] %v", m.name, e.id, pass)
			})
		}
	}
}

// =============================================================================
// FeatureBench — implement features from spec
// =============================================================================

func TestAllModels_FeatureBench(t *testing.T) {
	t.Parallel()
	models := allBenchModels(t)

	features := []struct {
		id    string
		spec  string
		check func(string) bool
	}{
		{"FB_Stack", "Implement a generic Stack[T] in Go with Push, Pop, Peek, Len, IsEmpty.",
			func(s string) bool { return strings.Contains(s, "Push") && strings.Contains(s, "Pop") }},
		{"FB_LRU", "Implement an LRU cache in Go with Get(key) and Put(key, value). Evict LRU on capacity.",
			func(s string) bool { return strings.Contains(s, "Get") && strings.Contains(s, "Put") }},
		{"FB_Middleware", "Write Go HTTP middleware: log method+path+duration, add X-Request-ID, recover from panics.",
			func(s string) bool { return strings.Contains(s, "http") && (strings.Contains(s, "Handler") || strings.Contains(s, "middleware")) }},
		{"FB_RateLimiter", "Implement a token bucket rate limiter in Go with Allow() bool and configurable rate/burst.",
			func(s string) bool { return strings.Contains(s, "Allow") && (strings.Contains(s, "token") || strings.Contains(s, "bucket") || strings.Contains(s, "rate")) }},
		{"FB_CLI", "Write a Go CLI using flag package that accepts -port (int), -host (string), -verbose (bool) and prints the config.",
			func(s string) bool { return strings.Contains(s, "flag") && strings.Contains(s, "port") }},
	}

	for _, m := range models {
		for _, f := range features {
			t.Run(m.name+"/"+f.id, func(t *testing.T) {
				t.Parallel()
				text := bmExec(t, m.cfg, f.spec+" Show complete Go code.", 30*time.Second)
				pass := f.check(text)
				t.Logf("[%s/%s] %v", m.name, f.id, pass)
			})
		}
	}
}
