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
	"syscall"
	"time"
)

// ServerConfig holds daemon startup parameters.
type ServerConfig struct {
	Port           int
	DataDir        string
	AuthToken      string
	MaxTasks       int
	WebhookSecret  string
}

// Server is the HTTP daemon.
type Server struct {
	cfg    ServerConfig
	store  *Store
	mux    *http.ServeMux
	logger *slog.Logger
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

	s := &Server{
		cfg:    cfg,
		store:  store,
		mux:    http.NewServeMux(),
		logger: logger,
	}
	s.registerRoutes()

	if cfg.WebhookSecret != "" {
		wh := NewWebhookHandler(store, cfg.WebhookSecret, logger)
		s.mux.HandleFunc(
			"POST /webhooks/github", wh.HandleWebhook,
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
	s.mux.HandleFunc("GET /tasks/{id}/sse", s.handleSSE)
}

// Run starts the HTTP server and blocks until shutdown.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.middleware(),
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

	s.logger.Info("daemon starting", "addr", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return s.store.Close()
}

func (s *Server) middleware() http.Handler {
	var h http.Handler = s.mux
	if s.cfg.AuthToken != "" {
		h = authMiddleware(s.cfg.AuthToken)(h)
	}
	h = recoveryMiddleware(s.logger)(h)
	h = requestIDMiddleware()(h)
	return h
}

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health, metrics, and webhooks bypass auth.
			// Webhooks use HMAC signature verification instead.
			if r.URL.Path == "/health" ||
				r.URL.Path == "/metrics" ||
				strings.HasPrefix(r.URL.Path, "/webhooks/") {
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
