package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jiayaoqijia/altcode/internal/workspace"
)

const (
	DefaultPollInterval = 10 * time.Second
	SpawnTimeout        = 30 * time.Second
	StuckThreshold      = 5 * time.Minute
	ActiveWindow        = 30 * time.Second
	ReadyThreshold      = 5 * time.Minute
	ActivityStaleness   = 5 * time.Minute
	MaxRestarts         = 2
)

// Manager drives the workspace lifecycle state machine.
type Manager struct {
	store    *workspace.Store
	plugins  workspace.PluginSet
	log      *slog.Logger
	interval time.Duration
}

// NewManager creates a lifecycle manager.
func NewManager(
	store *workspace.Store,
	plugins workspace.PluginSet,
	log *slog.Logger,
) *Manager {
	return &Manager{
		store:    store,
		plugins:  plugins,
		log:      log,
		interval: DefaultPollInterval,
	}
}

// SetInterval overrides the default poll interval (for tests).
func (m *Manager) SetInterval(d time.Duration) {
	m.interval = d
}

// Run polls a single session until it reaches a terminal state or
// the context is cancelled. Blocks the caller.
//
// On ctx.Done() we run a best-effort cleanup with a fresh short-lived
// context so worktrees, runtime handles, and agent subprocesses get
// torn down on SIGINT/SIGTERM. Without this, signal cancellation left
// the session in whatever non-terminal state it was in and accumulated
// orphaned worktrees on disk.
func (m *Manager) Run(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
) error {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			// Take sess.mu around the cleanup mutation just like the
			// normal Advance path does. The previous cleanup-on-cancel
			// path I added earlier mutated rec.WorktreePath /
			// sess.Status without the lock, racing with concurrent
			// SaveSession and wsctl readers. go test -race would catch
			// it under SIGINT mid-session.
			sess.Lock()
			cleanupErr := advanceCleanup(cleanupCtx, sess, m.plugins)
			sess.Unlock()
			if cleanupErr != nil {
				m.log.Error("cleanup on cancel", "err", cleanupErr, "id", sess.ID)
			}
			if err := m.store.SaveSession(sess); err != nil {
				m.log.Error("save on cancel", "err", err, "id", sess.ID)
			}
			cancel()
			return ctx.Err()
		case <-ticker.C:
			if err := m.Advance(ctx, sess); err != nil {
				m.log.Error("advance", "err", err,
					"id", sess.ID, "status", sess.Status)
			}
			if saveErr := m.store.SaveSession(sess); saveErr != nil {
				m.log.Error("save", "err", saveErr, "id", sess.ID)
			}
			if isTerminal(sess.Status) {
				return nil
			}
		}
	}
}

// isTerminal returns true for states that end the loop.
func isTerminal(s workspace.WorkspaceStatus) bool {
	return s == workspace.WSSDone || s == workspace.WSSFailed
}

// Advance drives one state-machine step for a session.
//
// Holds sess.mu around the entire transition so concurrent readers
// (wsctl injectors, /workspace status, store snapshots) don't see
// half-mutated state. Without this, lifecycle wrote to sess.Status
// and rec.ActivityState while wsctl was iterating sess.Agents under
// the same mutex — `go test -race` would catch the unsynchronized
// writes immediately.
func (m *Manager) Advance(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
) error {
	sess.Lock()
	defer sess.Unlock()
	switch sess.Status {
	case workspace.WSSSpawning:
		return advanceSpawning(ctx, sess, m.plugins)
	case workspace.WSSWorking:
		return advanceWorking(ctx, sess, m.plugins, m.store)
	case workspace.WSSPROpen:
		return advancePROpen(ctx, sess, m.plugins)
	case workspace.WSSCIChecking:
		return advanceCIChecking(ctx, sess, m.plugins)
	case workspace.WSSCIFailed:
		return advanceCIFailed(ctx, sess, m.plugins, m.store)
	case workspace.WSSChangesRequested:
		return advanceChangesRequested(ctx, sess, m.plugins, m.store)
	case workspace.WSSApproved:
		return advanceApproved(ctx, sess, m.plugins)
	case workspace.WSSMergeable:
		return advanceMergeable(ctx, sess, m.plugins)
	case workspace.WSSMerged:
		return advanceMerged(sess)
	case workspace.WSSCleanup:
		return advanceCleanup(ctx, sess, m.plugins)
	default:
		return fmt.Errorf("unhandled status: %s", sess.Status)
	}
}
