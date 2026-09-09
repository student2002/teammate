// server.go provides initialization and lifecycle management for the Teammate HTTP server.
// The server integrates all components: database connection, Redis cache, SSE Hub, WebSocket Gateway, and the scheduled task scheduler.
// It supports graceful shutdown: listening for SIGINT/SIGTERM signals and shutting down each component in turn before exiting.
//
// Route configuration is in routes.go and routes_*.go, registered via routeRegistrar.
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/db"
	"github.com/teammate/server/internal/scheduler"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/store"
)

// Server is the core struct of the Teammate HTTP server, managing the lifecycle of all server components.
type Server struct {
	// Config is the server configuration.
	Config *Config
	// DB is the PostgreSQL database connection.
	DB *sql.DB
	// Redis is the Redis client, used for caching, Pub/Sub, distributed locks, and rate limiting.
	Redis *redis.Client
	// Router is the Chi router, managing all HTTP routes and middleware.
	Router chi.Router
	// Hub is the SSE event hub, managing real-time event push between Agents and the Server.
	Hub *ws.Hub
	// Gateway is the WebSocket log gateway, managing real-time push of task execution logs.
	Gateway *ws.Gateway
	// Scheduler is the scheduled task scheduler, responsible for periodic tasks such as node timeout detection.
	Scheduler *scheduler.Scheduler
	// http is the underlying HTTP server instance.
	http *http.Server
}

// New creates a new Server instance, initializing the database, Redis, SSE Hub, WebSocket Gateway, and scheduler.
//
// Security checks:
//   - The default JWT secret (dev-secret-change-me) is forbidden in production
//   - Wildcard CORS origins (*) are forbidden in production
//   - The encryption key must be initialized (via the TEAMMATE_ENCRYPTION_KEY_BASE64 environment variable)
//
// Parameters:
//   - cfg: server configuration
//
// Returns:
//   - *Server: the initialized server instance
//   - error: an error returned when initialization fails
func New(cfg Config) (*Server, error) {
	// Security check: the default JWT secret is forbidden in production
	if cfg.JWTSecret == "dev-secret-change-me" && os.Getenv("TEAMMATE_DEV") != "true" {
		return nil, fmt.Errorf("TEAMS_JWT_SECRET must be changed from the default value for security")
	}

	if os.Getenv("TEAMMATE_DEV") == "true" {
		slog.Warn("running in development mode, security features relaxed")
	}

	// Security check: CORS origins must be explicitly configured in production
	if cfg.AllowedOrigins == "*" && os.Getenv("TEAMMATE_DEV") != "true" {
		return nil, fmt.Errorf("TEAMS_ALLOWED_ORIGINS must be configured in production (no wildcard *)")
	}

	// Initialize the encryption key from the environment variable
	if err := crypto.InitEncryptionKey(); err != nil {
		return nil, fmt.Errorf("encryption key initialization failed: %w", err)
	}

	pgDB, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr(cfg.RedisURL),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		pgDB.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	s := &Server{
		Config: &cfg,
		DB:     pgDB,
		Redis:  rdb,
	}

	s.Hub = ws.NewHub(rdb)
	s.Gateway = ws.NewGateway(rdb)

	st := store.New(s.DB)
	s.Scheduler = scheduler.NewScheduler(st, s.Hub, s.Redis)

	s.Router = s.setupRoutes()

	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      s.Router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s, nil
}

// Start starts the HTTP server, along with the SSE Hub, WebSocket Gateway, and scheduler.
// It listens for SIGINT/SIGTERM signals to perform graceful shutdown.
//
// Background components started:
//   - SSE Hub: listens to Redis Pub/Sub and dispatches events to local subscribers
//   - WebSocket Gateway: listens to Redis Pub/Sub and dispatches logs to local subscribers
//   - Scheduler: executes scheduled tasks such as node timeout detection
//
// Returns:
//   - error: an error returned when the server fails to start or receives a termination signal
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the SSE Hub (Redis Pub/Sub listener)
	go s.Hub.Start(ctx)

	// Start the WebSocket Gateway (Redis Pub/Sub log listener)
	go s.Gateway.Start(ctx)

	// Start the scheduler
	go s.Scheduler.Start(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		cancel()
		return err
	case sig := <-quit:
		slog.Info("shutting down", "signal", sig)
	}

	cancel()
	return s.Stop()
}

// Stop gracefully shuts down the HTTP server, closing the SSE Hub, WebSocket Gateway, Redis, and database connections in turn.
// It uses a 10-second timeout to ensure the shutdown process does not block indefinitely.
//
// Shutdown order:
//  1. HTTP server (wait for active connections to finish)
//  2. SSE Hub (close all client channels)
//  3. WebSocket Gateway (close all client channels)
//  4. Redis connection
//  5. PostgreSQL database connection
//
// Returns:
//   - error: an error returned if one occurs during shutdown
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.http.Shutdown(ctx); err != nil {
		slog.Error("http shutdown error", "err", err)
	}
	s.Hub.Close()
	s.Gateway.Close()
	if err := s.Redis.Close(); err != nil {
		slog.Error("redis close error", "err", err)
	}
	if s.DB != nil {
		s.DB.Close()
	}
	return nil
}
