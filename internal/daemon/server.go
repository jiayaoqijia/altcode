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

	"github.com/altcode-ai/altcode/internal/daemon/web"
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
	}
	orch := NewOrchestrator(store, OrchestratorConfig{
		SpawnFunc:   spawnFunc,
		Logger:      logger,
		PlanModel:   os.Getenv("ALTFIX_PLAN_MODEL"),
		ImplModel:   os.Getenv("ALTFIX_IMPL_MODEL"),
		ReviewModel: os.Getenv("ALTFIX_REVIEW_MODEL"),
		WorkDir:     cfg.DataDir,
	})

	s := &Server{
		cfg:    cfg,
		store:  store,
		mux:    http.NewServeMux(),
		logger: logger,
		cm:     cm,
		orch:   orch,
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
		sessions.StartGC(5 * time.Minute)
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

	go func() {
		<-shutdownCtx.Done()
		s.logger.Info("shutting down daemon")
		timeoutCtx, cancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer cancel()
		httpServer.Shutdown(timeoutCtx)
	}()

	go s.pollPendingTasks(ctx)

	s.logger.Info("daemon starting", "addr", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return s.store.Close()
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
func (s *Server) dispatchTask(task *Task) {
	// Atomically claim this task ID to prevent double-dispatch
	// from concurrent poller ticks or handleCreateTask goroutines.
	if _, loaded := s.runners.LoadOrStore(task.ID, (*TaskRunner)(nil)); loaded {
		return // already dispatched
	}

	ctx, cancel := context.WithCancel(context.Background())

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
	ticker := time.NewTicker(5 * time.Second)
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
				if _, ok := s.runners.Load(task.ID); ok {
					continue
				}
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
