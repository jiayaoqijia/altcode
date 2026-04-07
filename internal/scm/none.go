package scm

import (
	"context"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// NoopSCM implements workspace.SCM as a no-op.
// Used when no GitHub repo is configured.
type NoopSCM struct{}

func (n *NoopSCM) Name() string { return "none" }

func (n *NoopSCM) CreatePR(
	_ context.Context, _ workspace.CreatePRRequest,
) (*workspace.PR, error) {
	return nil, nil
}

func (n *NoopSCM) GetPR(
	_ context.Context, _ int,
) (*workspace.PR, error) {
	return nil, nil
}

func (n *NoopSCM) ListPRs(
	_ context.Context, _ string,
) ([]*workspace.PR, error) {
	return nil, nil
}

func (n *NoopSCM) GetPRReviews(
	_ context.Context, _ int,
) ([]*workspace.Review, error) {
	return nil, nil
}

func (n *NoopSCM) RequestReview(
	_ context.Context, _ int, _ []string,
) error {
	return nil
}

func (n *NoopSCM) MergePR(
	_ context.Context, _ int, _ workspace.MergeMethod,
) error {
	return nil
}

func (n *NoopSCM) CIStatus(
	_ context.Context, _ string,
) (workspace.CICheckStatus, error) {
	return workspace.CIUnknown, nil
}

func (n *NoopSCM) CILogs(
	_ context.Context, _ string,
) (string, error) {
	return "", nil
}
