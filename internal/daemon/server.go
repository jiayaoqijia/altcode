package daemon

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jiayaoqijia/altcode/internal/daemon/web"
)

// ServerConfig holds daemon startup parameters.
type ServerConfig struct {
	Port               int
	DataDir            string
	AuthToken          string
	MaxTasks           int
	WebhookSecret      string
	GitHubClientID     string
	GitHubClientSecret string
	AllowedOrgs        []string
	AllowedUsers       []string
	AdminUsers         []string
	SigningKey          string
	// TestMode enables /auth/test-login bypass for E2E tests.
	TestMode bool
	// BasePath is the external mount path for proxy deployments.
	BasePath string
	// CloudMode enables Cloud Claw session ticket auth.
	CloudMode bool
	// SessionSigningKey is the HMAC key for session tickets.
	SessionSigningKey string
	// TrustProxy enables X-Forwarded-Proto for TLS detection
	// behind a reverse proxy.
	TrustProxy bool
	// SpawnFunc overrides the default subprocess-based spawn.
	// Nil means use SpawnAgent + ReadAll + Wait from subprocess.go.
	SpawnFunc SpawnFunc
	// PollInterval is how often pollPendingTasks wakes to check the
	// pending queue. Zero = 5s (the production default). Tests use
	// e.g. 25*time.Millisecond to avoid 7s waits on every poll-test.
	// Karpathy autoresearch iter-1: shaves the slowest two tests.
	PollInterval time.Duration
	// SSEPollInterval is how often the SSE handler polls the store
	// for new events and emits a heartbeat. Zero = 2s default. Tests
	// override to ~25ms. Iter-2 of the autoresearch loop.
	SSEPollInterval time.Duration
}

// Server is the HTTP daemon.
type Server struct {
	cfg     ServerConfig
	store   *Store
	mux     *http.ServeMux
	logger  *slog.Logger
	cm      *ConcurrencyManager
	orch    *Orchestrator
	runners sync.Map // taskID -> *TaskRunner
	// stopSessionGC stops the web session GC goroutine on shutdown;
	// nil when the web UI is disabled.
	stopSessionGC func()
	// lifecycleCtx is the root context for all background tasks.
	// It is cancelled on daemon shutdown so runners inherit the signal
	// instead of running on orphaned context.Background() trees.
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	// dispatchWG tracks in-flight dispatchTask goroutines so shutdown
	// can wait for runner store-writes to drain before store.Close.
	dispatchWG sync.WaitGroup
}

// NewServer creates a daemon server.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = home + "/.altcode/daemon"
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	store, err := NewStore(cfg.DataDir + "/tasks.db")
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	maxTasks := cfg.MaxTasks
	if maxTasks < 1 {
		maxTasks = 2
	}
	cm := NewConcurrencyManager(maxTasks, logger)

	spawnFunc := cfg.SpawnFunc
	if spawnFunc == nil {
		spawnFunc = defaultSpawnFunc
		// In --test-mode keep tasks in active phases for the duration of
		// e2e interactions instead of letting the real altcode subprocess
		// fail fast (which leaves stop/steer endpoints returning 409).
		if cfg.TestMode {
			spawnFunc = testModeSpawnFunc
		}
	}
	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc:   spawnFunc,
		Logger:      logger,
		PlanModel:   os.Getenv("ALTFIX_PLAN_MODEL"),
		ImplModel:   os.Getenv("ALTFIX_IMPL_MODEL"),
		ReviewModel: os.Getenv("ALTFIX_REVIEW_MODEL"),
		WorkDir:     cfg.DataDir,
	})

	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	s := &Server{
		cfg:             cfg,
		store:           store,
		mux:             http.NewServeMux(),
		logger:          logger,
		cm:              cm,
		orch:            orch,
		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
	}
	s.registerRoutes()

	if cfg.WebhookSecret != "" {
		wh := NewWebhookHandler(store, cfg.WebhookSecret, logger)
		s.mux.HandleFunc(
			"POST /webhooks/github", wh.HandleWebhook,
		)
	}

	if cfg.GitHubClientID != "" || cfg.TestMode {
		sessions := web.NewSessionStore(8 * time.Hour)
		s.stopSessionGC = sessions.StartGC(5 * time.Minute)
		if err := web.RegisterRoutes(s.mux, web.WebConfig{
			Sessions:          sessions,
			GitHubClientID:    cfg.GitHubClientID,
			GitHubSecret:      cfg.GitHubClientSecret,
			AllowedOrgs:       cfg.AllowedOrgs,
			AllowedUsers:      cfg.AllowedUsers,
			AdminUsers:        cfg.AdminUsers,
			SigningKey:         []byte(cfg.SigningKey),
			Store:             &storeAdapter{s: store},
			BaseURL: fmt.Sprintf(
				"http://localhost:%d", cfg.Port,
			),
			TestMode:          cfg.TestMode,
			BasePath:          cfg.BasePath,
			CloudMode:         cfg.CloudMode,
			SessionSigningKey: []byte(cfg.SessionSigningKey),
			TrustProxy:        cfg.TrustProxy,
		}); err != nil {
			return nil, fmt.Errorf("web ui: %w", err)
		}
		logger.Info("web UI enabled",
			"login", fmt.Sprintf(
				"http://localhost:%d/ui/login", cfg.Port,
			),
		)
	}

	return s, nil
}

// Store returns the underlying store for external access (e.g.
// crash recovery in the cobra subcommand).
func (s *Server) Store() *Store { return s.store }

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /tasks", s.handleCreateTask)
	s.mux.HandleFunc("GET /tasks", s.handleListTasks)
	s.mux.HandleFunc("GET /tasks/{id}", s.handleGetTask)
	s.mux.HandleFunc("POST /tasks/{id}/stop", s.handleStopTask)
	s.mux.HandleFunc("POST /tasks/{id}/steer", s.handleSteerTask)
	s.mux.HandleFunc("GET /tasks/{id}/checkpoints", s.handleListCheckpoints)
	s.mux.HandleFunc("POST /tasks/{id}/restore", s.handleRestoreCheckpoint)
	s.mux.HandleFunc("GET /tasks/{id}/sse", s.handleSSE)
	s.mux.HandleFunc("GET /ws/{id}", s.handleWebSocket)
}

// storeAdapter wraps *Store and implements web.StoreIface,
// converting daemon types to web view types.
type storeAdapter struct {
	s *Store
}

func (a *storeAdapter) ListTasks() ([]*web.TaskView, error) {
	tasks, err := a.s.ListTasks()
	if err != nil {
		return nil, err
	}
	views := make([]*web.TaskView, len(tasks))
	for i, t := range tasks {
		views[i] = taskToView(t)
	}
	return views, nil
}

func (a *storeAdapter) GetTask(id string) (*web.TaskView, error) {
	t, err := a.s.GetTask(id)
	if err != nil {
		return nil, err
	}
	return taskToView(t), nil
}

func (a *storeAdapter) ListEvents(
	taskID string, afterID int64,
) ([]*web.EventView, error) {
	events, err := a.s.ListEvents(taskID, afterID)
	if err != nil {
		return nil, err
	}
	views := make([]*web.EventView, len(events))
	for i, e := range events {
		views[i] = &web.EventView{
			ID:        e.ID,
			TaskID:    e.TaskID,
			EventType: e.EventType,
			Data:      e.Data,
			CreatedAt: e.CreatedAt,
		}
	}
	return views, nil
}

func taskToView(t *Task) *web.TaskView {
	return &web.TaskView{
		ID:              t.ID,
		RepoURL:         t.RepoURL,
		TaskDescription: t.TaskDescription,
		Status:          t.Status,
		RepoOwner:       t.RepoOwner,
		RepoName:        t.RepoName,
		IssueNumber:     t.IssueNumber,
		APICostUSD:      t.APICostUSD,
		PRNumber:        t.PRNumber,
		PRURL:           t.PRURL,
		CreatedAt:       t.CreatedAt,
	}
}

// Run starts the HTTP server and blocks until shutdown.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           s.middleware(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// ReadTimeout omitted: it applies to the full request duration
		// and would kill SSE streaming connections after the timeout.
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	// Graceful shutdown on SIGTERM/SIGINT.
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// shutdownDone signals that httpServer.Shutdown has returned. We
	// must observe this BEFORE draining dispatchWG, otherwise a POST
	// /tasks handler in-flight during shutdown can call dispatchWG.Add
	// concurrently with Wait (sync.WaitGroup panic). Shutdown blocks
	// until all handlers return, so waiting on it closes the Add-race
	// window completely.
	shutdownDone := make(chan struct{})
	var shutdownOnce sync.Once
	closeShutdown := func() { shutdownOnce.Do(func() { close(shutdownDone) }) }

	go func() {
		<-shutdownCtx.Done()
		s.logger.Info("shutting down daemon")
		timeoutCtx, cancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer cancel()
		httpServer.Shutdown(timeoutCtx)
		closeShutdown()
	}()

	// Track the poller in dispatchWG so shutdown's Wait blocks until it
	// exits — otherwise a poll tick already inside its for-loop can call
	// `dispatchWG.Add` after Wait starts (panic: "Add called concurrently
	// with Wait") or after it returns (race with store.Close).
	s.dispatchWG.Add(1)
	go func() {
		defer s.dispatchWG.Done()
		s.pollPendingTasks(s.lifecycleCtx)
	}()

	s.logger.Info("daemon starting", "addr", addr)
	serverErr := httpServer.ListenAndServe()

	// If ListenAndServe returned without Shutdown being triggered
	// (e.g., bind failure), release the wait so we don't deadlock.
	if serverErr != nil && serverErr != http.ErrServerClosed {
		closeShutdown()
	}
	// Wait for all HTTP handlers to return (Shutdown handles this)
	// BEFORE we drain dispatchWG, so no handler can still race Add(1)
	// against our Wait(). Bounded by Shutdown's 10s grace.
	<-shutdownDone

	// Tear down in order: cancel runners + poller, wait for their
	// store-writes to drain, stop the web session GC goroutine, THEN
	// close the DB.
	s.lifecycleCancel()
	s.dispatchWG.Wait()
	if s.stopSessionGC != nil {
		s.stopSessionGC()
	}
	closeErr := s.store.Close()

	// Surface both server and store-close errors so a bad shutdown isn't
	// hidden by an earlier ListenAndServe error.
	if serverErr != nil && serverErr != http.ErrServerClosed {
		if closeErr != nil {
			return fmt.Errorf("serve: %w; store close: %v", serverErr, closeErr)
		}
		return serverErr
	}
	return closeErr
}

// testModeSpawnFunc keeps a task in the planning phase until its
// orchestrator context is cancelled (Stop) or a long ceiling elapses.
// This makes lifecycle e2e tests deterministic — Stop/Steer/SSE can
// run against a known-active task instead of racing a fast-failing
// real subprocess. Uses an explicit Timer (not time.After) so the
// timer fd is released on context cancellation rather than leaking
// for 5 minutes per cancelled task.
func testModeSpawnFunc(ctx context.Context, _ AgentConfig) (string, error) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return `{"steps":[]}`, nil
	}
}

// defaultSpawnFunc shells out via SpawnAgent from subprocess.go.
func defaultSpawnFunc(
	ctx context.Context, cfg AgentConfig,
) (string, error) {
	proc, err := SpawnAgent(ctx, cfg)
	if err != nil {
		return "", err
	}
	output, err := proc.ReadAll()
	if err != nil {
		return "", err
	}
	return output, proc.Wait()
}

// dispatchTask attempts to acquire a concurrency slot and run the
// task. If no slot is available the task stays pending for the
// background poller to pick up later.
//
// Callers MUST call s.dispatchWG.Add(1) BEFORE `go s.dispatchTask(...)`
// and pair it with the deferred Done here. Adding inside the goroutine
// body would race shutdown's Wait — Wait could see 0 and let store.Close
// run before the goroutine schedules in.
func (s *Server) dispatchTask(task *Task) {
	defer s.dispatchWG.Done()

	// Atomically claim this task ID to prevent double-dispatch
	// from concurrent poller ticks or handleCreateTask goroutines.
	if _, loaded := s.runners.LoadOrStore(task.ID, (*TaskRunner)(nil)); loaded {
		return // already dispatched
	}

	// Derive from the server lifecycle context so daemon shutdown
	// cancels in-flight tasks instead of letting them race with
	// store close.
	ctx, cancel := context.WithCancel(s.lifecycleCtx)

	if !s.cm.TryAcquire(task.ID, cancel) {
		// No slot — remove claim, cancel unused context, let poller retry.
		s.runners.Delete(task.ID)
		cancel()
		s.logger.Info("task queued — no slot available",
			"id", task.ID,
			"queue", s.cm.QueuePosition())
		return
	}

	runner := NewTaskRunner(task, s.store, s.orch, s.logger)
	s.runners.Store(task.ID, runner) // upgrade nil placeholder to real runner
	defer s.runners.Delete(task.ID)
	defer s.cm.Release(task.ID)

	runner.Run(ctx)
}

// pollPendingTasks periodically checks for pending tasks that
// have no active runner and dispatches them.
func (s *Server) pollPendingTasks(ctx context.Context) {
	interval := s.cfg.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tasks, err := s.store.ListTasksByStatus("pending")
			if err != nil {
				continue
			}
			for _, task := range tasks {
				// Re-check cancellation on each iteration: without this
				// a cancel that lands after ListTasksByStatus returns
				// would still dispatch every task in the batch, and
				// those goroutines would run MarkStarted/RunTask on an
				// already-cancelled context — turning pending tasks
				// into spurious "planning"/"failed" rows on shutdown.
				if ctx.Err() != nil {
					return
				}
				if _, ok := s.runners.Load(task.ID); ok {
					continue
				}
				s.dispatchWG.Add(1)
				go s.dispatchTask(task)
			}
		}
	}
}

func (s *Server) middleware() http.Handler {
	var h http.Handler = s.mux
	if s.cfg.AuthToken != "" {
		h = authMiddleware(s.cfg.AuthToken)(h)
	}
	h = bodySizeMiddleware(1 << 20)(h) // 1MB limit
	h = recoveryMiddleware(s.logger)(h)
	h = requestIDMiddleware()(h)
	return h
}

func bodySizeMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health, webhooks, and web UI paths bypass Bearer auth.
			// Webhooks use HMAC signature verification instead.
			// Web UI uses session-based auth via its own middleware.
			if r.URL.Path == "/health" ||
				strings.HasPrefix(r.URL.Path, "/webhooks/") ||
				strings.HasPrefix(r.URL.Path, "/ui/") ||
				strings.HasPrefix(r.URL.Path, "/auth/") ||
				strings.HasPrefix(r.URL.Path, "/share/") {
				next.ServeHTTP(w, r)
				return
			}
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, `{"error":"unauthorized"}`, 401)
				return
			}
			tokenBytes := []byte(auth[7:])
			expectedBytes := []byte(token)
			if subtle.ConstantTimeCompare(tokenBytes, expectedBytes) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, 401)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func recoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic in handler",
						"panic", rec, "path", r.URL.Path,
					)
					http.Error(w, `{"error":"internal server error"}`, 500)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func requestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = newID()[:8]
			}
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r)
		})
	}
}
