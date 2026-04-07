package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
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
func (m *Manager) Run(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
) error {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
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
func (m *Manager) Advance(
	ctx context.Context,
	sess *workspace.WorkspaceSession,
) error {
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
