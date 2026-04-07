package workspace

import (
	"context"
	"time"
)

// Runtime is where agents execute. Default: process (fork/exec).
// Alternative: tmux pane (for human observation), container.
type Runtime interface {
	// Spawn starts a command in the runtime, returning a handle.
	Spawn(ctx context.Context, cmd []string, env []string, workdir string) (RuntimeHandle, error)
	// Attach returns a reader that streams terminal output from an existing handle.
	Attach(ctx context.Context, handle RuntimeHandle) (<-chan string, error)
	// Kill terminates an active handle. Best-effort; may already be dead.
	Kill(handle RuntimeHandle) error
	// IsRunning checks whether the process is still alive.
	IsRunning(handle RuntimeHandle) (bool, error)
	// Name returns the runtime's identifier string, e.g. "process", "tmux".
	Name() string
}

// RuntimeHandle is an opaque reference to a running process.
// Implementations store whatever is needed (PID, tmux pane ID, container ID).
type RuntimeHandle struct {
	// ID is a stable, serializable identifier. Format is runtime-specific.
	// process runtime: "pid:1234"
	// tmux runtime:    "tmux:altcode-abc123:0"
	ID string
	// StartedAt is when the process was spawned.
	StartedAt time.Time
}

// Agent is a pluggable AI coding tool backend.
// Implementations: claude, codex, opencode, aider.
type Agent interface {
	// LaunchCommand returns the argv to start this agent for the given session.
	// The session carries workdir, branch, task prompt, and resume info.
	LaunchCommand(session *AgentSession) ([]string, error)
	// Environment returns additional env vars required by the agent.
	// Must not include API keys -- those come from the process environment.
	Environment(session *AgentSession) (map[string]string, error)
	// ActivityState returns the agent's current activity, consulting JSONL
	// and/or the native API. Returns nil if no data is available yet.
	ActivityState(ctx context.Context, session *AgentSession) (*ActivityDetection, error)
	// IsProcessRunning checks whether the agent process is still alive.
	IsProcessRunning(session *AgentSession) (bool, error)
	// SessionInfo extracts summary, cost, and session ID from the agent's
	// native data structures. Returns nil if the agent has no introspection.
	SessionInfo(ctx context.Context, session *AgentSession) (*AgentSessionInfo, error)
	// SetupWorkspaceHooks installs hooks that allow the workspace to track
	// metadata (PR numbers, commit hashes) without parsing agent output.
	SetupWorkspaceHooks(session *AgentSession) error
	// RestoreCommand returns the argv to resume a prior session, or nil if
	// the backend has no session resume capability.
	RestoreCommand(session *AgentSession) ([]string, error)
	// Name returns the backend identifier, e.g. "claude", "codex".
	Name() string
}

// AgentSession carries all context needed to launch or restore an agent.
type AgentSession struct {
	// WorkspacePath is the root of the .altcode/workspace/{id} directory.
	WorkspacePath string
	// WorktreePath is the isolated git worktree root for this agent.
	WorktreePath string
	// Branch is the agent's git branch (e.g. "altcode/architect/auth-design").
	Branch string
	// Task is the high-level task description.
	Task string
	// Role is this agent's role (e.g. "architect", "implementer").
	Role string
	// Model is the AI model to use, e.g. "anthropic/claude-sonnet-4-20250514".
	// Empty means use the default for the backend.
	Model string
	// MaxTurns limits the agent's turn count. 0 means no limit.
	MaxTurns int
	// SystemPromptAppend is appended to the agent's system prompt, injecting
	// role-specific instructions without replacing the base system prompt.
	SystemPromptAppend string
	// PriorSessionID is the session ID from a previous run, for --resume.
	PriorSessionID string
	// Env contains the process environment (API keys, PATH, etc.).
	// The Agent.Environment() additions are merged on top of this.
	Env []string
	// AOSessionID is the workspace session ID, injected as env var.
	AOSessionID string
	// RuntimeHandleID is the opaque runtime handle (e.g. "pid:1234") for
	// process liveness checks. Set after spawn, empty before.
	RuntimeHandleID string
}

// AgentSessionInfo holds extracted metadata from an agent's native data.
type AgentSessionInfo struct {
	Summary   string
	Cost      float64 // USD
	SessionID string  // for --resume on next invocation
	Tokens    int
}

// Workspace manages code isolation per agent.
// Default: git worktree. Alternative: full clone.
type Workspace interface {
	// Setup creates an isolated workspace for an agent.
	Setup(ctx context.Context, req WorkspaceSetupRequest) (*WorkspaceSetupResult, error)
	// Teardown removes the isolated workspace when done.
	Teardown(ctx context.Context, path string) error
	// Checkpoint creates a git commit capturing the current workspace state.
	Checkpoint(ctx context.Context, path string, msg string) (string, error)
	// Name returns the workspace type, e.g. "worktree", "clone".
	Name() string
}

// WorkspaceSetupRequest specifies what to create.
type WorkspaceSetupRequest struct {
	// GitRoot is the path to the bare .git or the main checkout root.
	GitRoot string
	// WorktreePath is where to create the worktree/clone.
	WorktreePath string
	// Branch is the new branch name to create.
	Branch string
	// BaseRef is the git ref to branch from (e.g. "main", "origin/main").
	BaseRef string
	// SymlinkDeps is a list of directories to symlink rather than copy.
	SymlinkDeps []string
}

// WorkspaceSetupResult holds the outcome of a workspace setup.
type WorkspaceSetupResult struct {
	// Path is the absolute path to the workspace root.
	Path string
	// Branch is the actual branch name created.
	Branch string
	// BaseCommit is the git hash at branch creation time.
	BaseCommit string
}

// Tracker is an issue tracker integration. Default: github issues.
type Tracker interface {
	// GetIssue fetches issue details by ID.
	GetIssue(ctx context.Context, id string) (*Issue, error)
	// UpdateIssue sets a label or comment on an issue.
	UpdateIssue(ctx context.Context, id string, update IssueUpdate) error
	// Name returns the tracker identifier, e.g. "github", "linear", "none".
	Name() string
}

// Issue holds issue metadata from the tracker.
type Issue struct {
	ID          string
	Title       string
	Description string
	Labels      []string
	URL         string
}

// IssueUpdate describes a modification to an issue.
type IssueUpdate struct {
	AddLabel    string
	RemoveLabel string
	Comment     string
}

// SCM is a source control management integration.
type SCM interface {
	// CreatePR creates a pull request from the agent's branch to the base.
	CreatePR(ctx context.Context, req CreatePRRequest) (*PR, error)
	// GetPR fetches current PR state including CI and review status.
	GetPR(ctx context.Context, id int) (*PR, error)
	// ListPRs returns open PRs whose heads start with the given prefix.
	ListPRs(ctx context.Context, headPrefix string) ([]*PR, error)
	// GetPRReviews returns review comments on a PR.
	GetPRReviews(ctx context.Context, prID int) ([]*Review, error)
	// RequestReview requests a review from specific reviewers.
	RequestReview(ctx context.Context, prID int, reviewers []string) error
	// MergePR merges the PR using the given method.
	MergePR(ctx context.Context, prID int, method MergeMethod) error
	// CIStatus polls the combined CI status for a commit.
	CIStatus(ctx context.Context, sha string) (CICheckStatus, error)
	// Name returns "github", "gitlab", etc.
	Name() string
}

// CreatePRRequest specifies what PR to create.
type CreatePRRequest struct {
	Title  string
	Body   string
	Head   string // branch name
	Base   string // target branch (e.g. "main")
	Draft  bool
	Labels []string
}

// PR holds pull request metadata.
type PR struct {
	ID             int
	Number         int
	Title          string
	URL            string
	HeadSHA        string
	HeadBranch     string
	BaseBranch     string
	State          string // "open", "closed", "merged"
	Draft          bool
	CIStatus       CICheckStatus
	ReviewStatus   ReviewStatus
	MergeableState string // "mergeable", "conflicting", "unknown", "blocked"
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Review holds a PR review.
type Review struct {
	ID          int
	Author      string
	State       string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED"
	Body        string
	Comments    []ReviewComment
	SubmittedAt time.Time
}

// ReviewComment is a single inline comment on a PR diff.
type ReviewComment struct {
	Path   string
	Line   int
	Body   string
	Author string
}

// CICheckStatus is the combined status of all CI checks.
type CICheckStatus string

const (
	CIUnknown CICheckStatus = "unknown"
	CIPending CICheckStatus = "pending"
	CIRunning CICheckStatus = "running"
	CIPass    CICheckStatus = "pass"
	CIFail    CICheckStatus = "fail"
	CISkipped CICheckStatus = "skipped"
)

// ReviewStatus is the aggregated review state of a PR.
type ReviewStatus string

const (
	ReviewNone             ReviewStatus = "none"
	ReviewApproved         ReviewStatus = "approved"
	ReviewChangesRequested ReviewStatus = "changes_requested"
	ReviewCommented        ReviewStatus = "commented"
)

// MergeMethod controls how a PR is merged.
type MergeMethod string

const (
	MergeMerge  MergeMethod = "merge"
	MergeSquash MergeMethod = "squash"
	MergeRebase MergeMethod = "rebase"
)

// Notifier delivers notifications to operators.
type Notifier interface {
	// Notify sends a notification for the given workspace event.
	Notify(ctx context.Context, event NotifyEvent) error
	// Name returns "desktop", "slack", "webhook", "none".
	Name() string
}

// NotifyEvent carries the payload for a notification.
type NotifyEvent struct {
	WorkspaceID string
	Role        string
	Kind        NotifyKind
	Summary     string
	URL         string
}

// NotifyKind identifies what happened.
type NotifyKind string

const (
	NotifyNeedsAttention NotifyKind = "needs_attention"
	NotifyPROpen         NotifyKind = "pr_open"
	NotifyCIFailed       NotifyKind = "ci_failed"
	NotifyApproved       NotifyKind = "approved"
	NotifyMerged         NotifyKind = "merged"
	NotifyError          NotifyKind = "error"
)

// Terminal is the human-attachment mechanism.
type Terminal interface {
	// Open opens a terminal window/pane showing the agent's runtime.
	Open(ctx context.Context, handle RuntimeHandle, title string) error
	// Name returns "iterm2", "tmux", "none".
	Name() string
}

// Lifecycle is the core state machine and polling loop.
// Implementation: internal/lifecycle/lifecycle.go
type Lifecycle interface {
	// Advance checks the current state of a session and drives transitions.
	Advance(ctx context.Context, session *WorkspaceSession, plugins PluginSet) error
}

// PluginSet holds the resolved plugin instances for a workspace.
type PluginSet struct {
	Runtime   Runtime
	Agents    map[string]Agent // keyed by backend name: "claude", "codex", etc.
	Workspace Workspace
	Tracker   Tracker
	SCM       SCM
	Notifier  Notifier
	Terminal  Terminal
}
