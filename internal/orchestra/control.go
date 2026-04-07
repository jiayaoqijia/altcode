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
	Outputs   map[string]string // role -> output
	SessionID string
}
