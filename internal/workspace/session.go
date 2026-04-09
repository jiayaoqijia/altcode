package workspace

import (
	"sync"
	"time"
)

// WorkspaceStatus is the coarse-grained lifecycle state of the whole workspace.
type WorkspaceStatus string

const (
	WSSSpawning         WorkspaceStatus = "spawning"
	WSSWorking          WorkspaceStatus = "working"
	WSSPROpen           WorkspaceStatus = "pr_open"
	WSSCIChecking       WorkspaceStatus = "ci_checking"
	WSSCIFailed         WorkspaceStatus = "ci_failed"
	WSSChangesRequested WorkspaceStatus = "changes_requested"
	WSSApproved         WorkspaceStatus = "approved"
	WSSMergeable        WorkspaceStatus = "mergeable"
	WSSMerged           WorkspaceStatus = "merged"
	WSSCleanup          WorkspaceStatus = "cleanup"
	WSSDone             WorkspaceStatus = "done"
	WSSFailed           WorkspaceStatus = "failed"
	WSSPaused           WorkspaceStatus = "paused"
)

// ActivityState is the fine-grained activity of a single agent process.
type ActivityState string

const (
	ActivitySpawning  ActivityState = "spawning"
	ActivityActive    ActivityState = "active"
	ActivityReady     ActivityState = "ready"
	ActivityIdle      ActivityState = "idle"
	ActivityWaitInput ActivityState = "waiting_input"
	ActivityBlocked   ActivityState = "blocked"
	ActivityExited    ActivityState = "exited"
)

// ActivityDetection is the result of checking an agent's current activity.
// Errata E1: this struct is returned by Agent.ActivityState and by the
// activity detection cascade in internal/activity/.
type ActivityDetection struct {
	State     ActivityState
	Timestamp time.Time
	Source    string // "process_dead", "jsonl_actionable", "jsonl_age", "native_signal"
}

// WorkspaceSession is the central record for one workspace run.
// It is persisted to .altcode/workspace/{id}/session.json.
type WorkspaceSession struct {
	ID           string                 `json:"id"`
	Task         string                 `json:"task"`
	Status       WorkspaceStatus        `json:"status"`
	GitRoot      string                 `json:"git_root"`
	BaseBranch   string                 `json:"base_branch"`
	WorkflowName string                 `json:"workflow_name,omitempty"`
	Agents       map[string]*AgentRecord `json:"agents"`
	CIRetries    int                    `json:"ci_retries"`
	MaxCIRetries int                    `json:"max_ci_retries"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	Error        string                 `json:"error,omitempty"`
	AutoMerge    bool                   `json:"auto_merge,omitempty"`
	MergeMethod  MergeMethod            `json:"merge_method,omitempty"`
	Reviewers    []string               `json:"reviewers,omitempty"`

	mu sync.Mutex `json:"-"` //nolint:unused // guards in-memory mutations
}

// Lock acquires the session mutex for cross-goroutine field mutations.
func (s *WorkspaceSession) Lock()   { s.mu.Lock() }
// Unlock releases the session mutex.
func (s *WorkspaceSession) Unlock() { s.mu.Unlock() }

// AgentRecord tracks one agent within a workspace.
// R3-2: canonical definition including RestartCount and LastCheckedSHA from errata.
type AgentRecord struct {
	Role              string        `json:"role"`
	Backend           string        `json:"backend"`
	Model             string        `json:"model"`
	Branch            string        `json:"branch"`
	WorktreePath      string        `json:"worktree_path"`
	RuntimeHandleID   string        `json:"runtime_handle_id"`
	SessionID         string        `json:"session_id,omitempty"`
	PRID              int           `json:"pr_id,omitempty"`
	PRURL             string        `json:"pr_url,omitempty"`
	HeadSHA           string        `json:"head_sha,omitempty"`
	LastCheckedSHA    string        `json:"last_checked_sha,omitempty"`
	ActivityState     ActivityState `json:"activity_state"`
	ActivityUpdatedAt time.Time     `json:"activity_updated_at"`
	CIStatus          CICheckStatus `json:"ci_status"`
	ReviewStatus      ReviewStatus  `json:"review_status"`
	SpawnedAt         time.Time     `json:"spawned_at"`
	ExitedAt          *time.Time    `json:"exited_at,omitempty"`
	ExitCode          int           `json:"exit_code"`
	TurnCount         int           `json:"turn_count"`
	CostUSD           float64       `json:"cost_usd"`
	RestartCount      int           `json:"restart_count"`
}

// AttentionPriority classifies how urgently an agent needs operator attention.
type AttentionPriority int

const (
	AttentionGreen  AttentionPriority = 0 // working normally
	AttentionYellow AttentionPriority = 1 // auto-fix failed, monitoring
	AttentionOrange AttentionPriority = 2 // PR ready, review needed
	AttentionRed    AttentionPriority = 3 // stuck, needs immediate intervention
)

// Priority computes the operator attention priority for an agent.
func (a *AgentRecord) Priority() AttentionPriority {
	switch a.ActivityState {
	case ActivityWaitInput, ActivityBlocked:
		return AttentionRed
	case ActivityExited:
		if a.ExitCode != 0 {
			return AttentionRed
		}
		if a.PRID > 0 && a.ReviewStatus == ReviewChangesRequested {
			return AttentionOrange
		}
		return AttentionGreen
	}
	if a.CIStatus == CIFail {
		return AttentionYellow
	}
	if a.PRID > 0 && a.ReviewStatus == ReviewNone {
		return AttentionOrange
	}
	return AttentionGreen
}
