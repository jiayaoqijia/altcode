package permission

import "sync"

// Mode determines the permission evaluation strategy.
type Mode int

const (
	ModeDefault Mode = iota
	ModeAuto
	ModeBypass
	ModePlan
)

// ActionType is the result of a permission check.
type ActionType int

const (
	ActionAllow ActionType = iota
	ActionDeny
	ActionAsk
)

// Rule maps a tool+pattern to an action.
type Rule struct {
	Tool    string
	Pattern string
	Action  ActionType
	Source  string // "cli", "session", "project", "user", "default"
}

// Evaluator checks whether tool calls are permitted.
type Evaluator struct {
	mode         Mode
	projectRoot  string
	rules        []Rule
	sessionRules []Rule
	callHistory  []callRecord
	mu           sync.Mutex
}

type callRecord struct {
	tool    string
	pattern string
}

// NewEvaluator creates an Evaluator with the given mode and rules.
func NewEvaluator(mode Mode, projectRoot string, rules []Rule) *Evaluator {
	allRules := append(DefaultRules(), rules...)
	return &Evaluator{
		mode:        mode,
		projectRoot: projectRoot,
		rules:       allRules,
	}
}

// Check evaluates whether a tool call is allowed.
func (e *Evaluator) Check(toolName, pattern string) ActionType {
	return e.CheckWithReadOnly(toolName, pattern, false)
}

// CheckWithReadOnly evaluates permission, considering read-only status.
func (e *Evaluator) CheckWithReadOnly(toolName, pattern string, readOnly bool) ActionType {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.mode == ModeBypass {
		return ActionAllow
	}

	if e.mode == ModePlan && !readOnly {
		return ActionDeny
	}

	if e.isDoomLoop(toolName, pattern) {
		return ActionAsk
	}

	// Check deny rules first
	for _, r := range e.sessionRules {
		if r.Action == ActionDeny && matchRule(r, toolName, pattern) {
			return ActionDeny
		}
	}
	for _, r := range e.rules {
		if r.Action == ActionDeny && matchRule(r, toolName, pattern) {
			return ActionDeny
		}
	}

	// Check allow rules
	for _, r := range e.sessionRules {
		if r.Action == ActionAllow && matchRule(r, toolName, pattern) {
			return ActionAllow
		}
	}
	for _, r := range e.rules {
		if r.Action == ActionAllow && matchRule(r, toolName, pattern) {
			return ActionAllow
		}
	}

	switch e.mode {
	case ModeAuto:
		return ActionDeny
	default:
		return ActionAsk
	}
}

// RecordCall records a tool call for doom loop detection.
func (e *Evaluator) RecordCall(toolName, pattern string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.callHistory = append(e.callHistory, callRecord{
		tool: toolName, pattern: pattern,
	})
}

// AddSessionRule adds a rule valid for this session only.
func (e *Evaluator) AddSessionRule(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionRules = append(e.sessionRules, r)
}

// SetMode changes the permission mode.
func (e *Evaluator) SetMode(mode Mode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mode = mode
}

func (e *Evaluator) isDoomLoop(toolName, pattern string) bool {
	n := len(e.callHistory)
	if n < 3 {
		return false
	}
	for i := n - 3; i < n; i++ {
		if e.callHistory[i].tool != toolName || e.callHistory[i].pattern != pattern {
			return false
		}
	}
	return true
}
