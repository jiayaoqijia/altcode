package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/altcode-ai/altcode/internal/workspace"
)

// --- mock implementations ---

type mockRuntime struct {
	running map[string]bool
}

func (m *mockRuntime) Spawn(_ context.Context, cmd []string, _ []string, _ string) (workspace.RuntimeHandle, error) {
	id := fmt.Sprintf("pid:%s", cmd[0])
	m.running[id] = true
	return workspace.RuntimeHandle{ID: id, StartedAt: time.Now()}, nil
}

func (m *mockRuntime) Attach(_ context.Context, _ workspace.RuntimeHandle) (<-chan string, error) {
	return nil, nil
}

func (m *mockRuntime) Kill(h workspace.RuntimeHandle) error {
	m.running[h.ID] = false
	return nil
}

func (m *mockRuntime) IsRunning(h workspace.RuntimeHandle) (bool, error) {
	return m.running[h.ID], nil
}

func (m *mockRuntime) Name() string { return "mock" }

type mockAgent struct {
	activity *workspace.ActivityDetection
}

func (m *mockAgent) LaunchCommand(_ *workspace.AgentSession) ([]string, error) {
	return []string{"mock-agent"}, nil
}

func (m *mockAgent) Environment(_ *workspace.AgentSession) (map[string]string, error) {
	return nil, nil
}

func (m *mockAgent) ActivityState(_ context.Context, _ *workspace.AgentSession) (*workspace.ActivityDetection, error) {
	return m.activity, nil
}

func (m *mockAgent) IsProcessRunning(_ *workspace.AgentSession) (bool, error) {
	return true, nil
}

func (m *mockAgent) SessionInfo(_ context.Context, _ *workspace.AgentSession) (*workspace.AgentSessionInfo, error) {
	return nil, nil
}

func (m *mockAgent) SetupWorkspaceHooks(_ *workspace.AgentSession) error { return nil }

func (m *mockAgent) RestoreCommand(_ *workspace.AgentSession) ([]string, error) {
	return []string{"mock-agent", "--resume"}, nil
}

func (m *mockAgent) Name() string { return "mock" }

type mockWorkspace struct{}

func (m *mockWorkspace) Setup(_ context.Context, _ workspace.WorkspaceSetupRequest) (*workspace.WorkspaceSetupResult, error) {
	return &workspace.WorkspaceSetupResult{}, nil
}

func (m *mockWorkspace) Teardown(_ context.Context, _ string) error { return nil }

func (m *mockWorkspace) Checkpoint(_ context.Context, _ string, _ string) (string, error) {
	return "abc123", nil
}

func (m *mockWorkspace) Name() string { return "mock" }

type mockSCM struct {
	prs      map[int]*workspace.PR
	ciStatus workspace.CICheckStatus
	reviews  []*workspace.Review
	merged   map[int]bool
}

func (m *mockSCM) CreatePR(_ context.Context, _ workspace.CreatePRRequest) (*workspace.PR, error) {
	return &workspace.PR{ID: 1}, nil
}

func (m *mockSCM) GetPR(_ context.Context, id int) (*workspace.PR, error) {
	pr, ok := m.prs[id]
	if !ok {
		return nil, fmt.Errorf("PR %d not found", id)
	}
	if m.merged[id] {
		pr.State = "merged"
	}
	return pr, nil
}

func (m *mockSCM) ListPRs(_ context.Context, _ string) ([]*workspace.PR, error) {
	return nil, nil
}

func (m *mockSCM) GetPRReviews(_ context.Context, _ int) ([]*workspace.Review, error) {
	return m.reviews, nil
}

func (m *mockSCM) RequestReview(_ context.Context, _ int, _ []string) error { return nil }

func (m *mockSCM) MergePR(_ context.Context, id int, _ workspace.MergeMethod) error {
	if m.merged == nil {
		m.merged = map[int]bool{}
	}
	m.merged[id] = true
	return nil
}

func (m *mockSCM) CIStatus(_ context.Context, _ string) (workspace.CICheckStatus, error) {
	return m.ciStatus, nil
}

func (m *mockSCM) CILogs(_ context.Context, _ string) (string, error) {
	return "mock CI failure: test_foo.go:42 expected 1 got 2", nil
}

func (m *mockSCM) Name() string { return "mock" }

type mockTracker struct{}

func (m *mockTracker) GetIssue(_ context.Context, _ string) (*workspace.Issue, error) {
	return nil, nil
}

func (m *mockTracker) UpdateIssue(_ context.Context, _ string, _ workspace.IssueUpdate) error {
	return nil
}

func (m *mockTracker) Name() string { return "mock" }

type mockNotifier struct{}

func (m *mockNotifier) Notify(_ context.Context, _ workspace.NotifyEvent) error { return nil }
func (m *mockNotifier) Name() string                                            { return "mock" }

type mockTerminal struct{}

func (m *mockTerminal) Open(_ context.Context, _ workspace.RuntimeHandle, _ string) error {
	return nil
}

func (m *mockTerminal) Name() string { return "mock" }

// --- helpers ---

func testPlugins(rt *mockRuntime, scm *mockSCM) workspace.PluginSet {
	return workspace.PluginSet{
		Runtime:   rt,
		Agents:    map[string]workspace.Agent{"mock": &mockAgent{}},
		Workspace: &mockWorkspace{},
		Tracker:   &mockTracker{},
		SCM:       scm,
		Notifier:  &mockNotifier{},
		Terminal:  &mockTerminal{},
	}
}

func testStore(t *testing.T) *workspace.Store {
	t.Helper()
	dir := t.TempDir()
	return workspace.NewStore(dir)
}

func testSession() *workspace.WorkspaceSession {
	return &workspace.WorkspaceSession{
		ID:           "test-ws-1",
		Task:         "implement auth",
		Status:       workspace.WSSSpawning,
		MaxCIRetries: 3,
		Agents: map[string]*workspace.AgentRecord{
			"implementer": {
				Role:            "implementer",
				Backend:         "mock",
				Branch:          "altcode/impl",
				RuntimeHandleID: "pid:1",
				SpawnedAt:       time.Now(),
				ActivityState:   workspace.ActivitySpawning,
			},
		},
	}
}

func testManager(
	t *testing.T,
	plugins workspace.PluginSet,
) *Manager {
	t.Helper()
	store := testStore(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return NewManager(store, plugins, log)
}

// --- tests ---

func TestAdvanceSpawning_AllAlive(t *testing.T) {
	rt := &mockRuntime{running: map[string]bool{"pid:1": true}}
	scm := &mockSCM{prs: map[int]*workspace.PR{}}
	mgr := testManager(t, testPlugins(rt, scm))
	sess := testSession()

	if err := mgr.Advance(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.Status != workspace.WSSWorking {
		t.Fatalf("expected working, got %s", sess.Status)
	}
}

func TestAdvanceSpawning_Timeout(t *testing.T) {
	rt := &mockRuntime{running: map[string]bool{"pid:1": false}}
	scm := &mockSCM{prs: map[int]*workspace.PR{}}
	mgr := testManager(t, testPlugins(rt, scm))
	sess := testSession()
	sess.Agents["implementer"].SpawnedAt = time.Now().Add(-SpawnTimeout - time.Second)

	if err := mgr.Advance(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.Status != workspace.WSSFailed {
		t.Fatalf("expected failed, got %s", sess.Status)
	}
	if sess.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestAdvanceWorking_DetectPR(t *testing.T) {
	rt := &mockRuntime{running: map[string]bool{"pid:1": true}}
	scm := &mockSCM{prs: map[int]*workspace.PR{}}
	mgr := testManager(t, testPlugins(rt, scm))
	sess := testSession()
	sess.Status = workspace.WSSWorking
	sess.Agents["implementer"].PRID = 42
	sess.Agents["implementer"].PRURL = "https://github.com/test/repo/pull/42"

	if err := mgr.Advance(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.Status != workspace.WSSPROpen {
		t.Fatalf("expected pr_open, got %s", sess.Status)
	}
}

func TestAdvanceCIChecking_Pass(t *testing.T) {
	scm := &mockSCM{
		ciStatus: workspace.CIPass,
		prs: map[int]*workspace.PR{
			42: {
				ID:           42,
				HeadSHA:      "sha1",
				CIStatus:     workspace.CIPass,
				ReviewStatus: workspace.ReviewApproved,
			},
		},
	}
	rt := &mockRuntime{running: map[string]bool{"pid:1": true}}
	mgr := testManager(t, testPlugins(rt, scm))
	sess := testSession()
	sess.Status = workspace.WSSCIChecking
	sess.Agents["implementer"].PRID = 42
	sess.Agents["implementer"].HeadSHA = "sha1"

	if err := mgr.Advance(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.Status != workspace.WSSApproved {
		t.Fatalf("expected approved, got %s", sess.Status)
	}
}

func TestAdvanceCIChecking_Fail(t *testing.T) {
	scm := &mockSCM{
		ciStatus: workspace.CIFail,
		prs:     map[int]*workspace.PR{},
	}
	rt := &mockRuntime{running: map[string]bool{"pid:1": true}}
	mgr := testManager(t, testPlugins(rt, scm))
	sess := testSession()
	sess.Status = workspace.WSSCIChecking
	sess.Agents["implementer"].PRID = 42
	sess.Agents["implementer"].HeadSHA = "sha1"

	if err := mgr.Advance(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.Status != workspace.WSSCIFailed {
		t.Fatalf("expected ci_failed, got %s", sess.Status)
	}
}

func TestAdvanceCIFailed_Retry(t *testing.T) {
	scm := &mockSCM{
		ciStatus: workspace.CIFail,
		prs:     map[int]*workspace.PR{},
	}
	rt := &mockRuntime{running: map[string]bool{"pid:1": true}}
	store := testStore(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	plugins := testPlugins(rt, scm)
	mgr := NewManager(store, plugins, log)
	sess := testSession()
	sess.Status = workspace.WSSCIFailed
	sess.CIRetries = 0
	sess.MaxCIRetries = 3
	// Save so SendMessage can load it
	if err := store.SaveSession(sess); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Advance(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.Status != workspace.WSSWorking {
		t.Fatalf("expected working, got %s", sess.Status)
	}
	if sess.CIRetries != 1 {
		t.Fatalf("expected CIRetries=1, got %d", sess.CIRetries)
	}
}

func TestAdvanceCIFailed_Exhausted(t *testing.T) {
	scm := &mockSCM{prs: map[int]*workspace.PR{}}
	rt := &mockRuntime{running: map[string]bool{"pid:1": true}}
	mgr := testManager(t, testPlugins(rt, scm))
	sess := testSession()
	sess.Status = workspace.WSSCIFailed
	sess.CIRetries = 3
	sess.MaxCIRetries = 3

	if err := mgr.Advance(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sess.Status != workspace.WSSFailed {
		t.Fatalf("expected failed, got %s", sess.Status)
	}
	if sess.Error == "" {
		t.Fatal("expected error message on exhaustion")
	}
}

func TestAggregateWorkspaceStatus(t *testing.T) {
	tests := []struct {
		name   string
		agents map[string]*workspace.AgentRecord
		want   workspace.WorkspaceStatus
	}{
		{
			name: "all working",
			agents: map[string]*workspace.AgentRecord{
				"a": {ActivityState: workspace.ActivityActive},
				"b": {ActivityState: workspace.ActivityReady},
			},
			want: workspace.WSSWorking,
		},
		{
			name: "one failed",
			agents: map[string]*workspace.AgentRecord{
				"a": {ActivityState: workspace.ActivityExited, ExitCode: 1},
				"b": {
					ActivityState: workspace.ActivityActive,
					PRID:          1,
					ReviewStatus:  workspace.ReviewApproved,
				},
			},
			want: workspace.WSSFailed,
		},
		{
			name: "one ci_failed, one working",
			agents: map[string]*workspace.AgentRecord{
				"a": {CIStatus: workspace.CIFail},
				"b": {ActivityState: workspace.ActivityActive},
			},
			want: workspace.WSSCIFailed,
		},
		{
			name: "changes_requested wins over ci_checking",
			agents: map[string]*workspace.AgentRecord{
				"a": {
					ReviewStatus: workspace.ReviewChangesRequested,
				},
				"b": {CIStatus: workspace.CIRunning},
			},
			want: workspace.WSSChangesRequested,
		},
		{
			name: "all approved",
			agents: map[string]*workspace.AgentRecord{
				"a": {
					PRID:         1,
					ReviewStatus: workspace.ReviewApproved,
				},
				"b": {
					PRID:         2,
					ReviewStatus: workspace.ReviewApproved,
				},
			},
			want: workspace.WSSApproved,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := &workspace.WorkspaceSession{
				Agents: tt.agents,
			}
			got := aggregateWorkspaceStatus(sess)
			if got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestExtractReviewContext(t *testing.T) {
	reviews := []*workspace.Review{
		{
			Comments: []workspace.ReviewComment{
				{Path: "main.go", Line: 10, Body: "fix this"},
				{Path: "auth.go", Line: 5, Body: "add check"},
			},
		},
	}
	got := ExtractReviewContext(reviews, "implement auth")
	if got == "" {
		t.Fatal("expected non-empty context")
	}
	for _, want := range []string{
		"main.go", "auth.go", "fix this", "add check",
	} {
		if !contains(got, want) {
			t.Fatalf("expected %q in output", want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
