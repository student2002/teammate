// router_factory.go provides the router constructor shared by the production server and tests.
// Dependencies are injected via RouterDeps to ensure the test router uses the same route configuration
// as the production router, preventing route drift.
package server

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/clock"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
)

// RouterDeps holds the dependencies required to construct the router.
// Production and test environments share this struct to ensure consistent route configuration.
type RouterDeps struct {
	// Config is the server configuration.
	Config Config
	// DB is the PostgreSQL database connection.
	DB *sql.DB
	// Redis is the Redis client; when nil, rate limiting falls back to the in-memory degraded mode.
	Redis *redis.Client
	// Hub is the SSE event hub.
	Hub *ws.Hub
	// Gateway is the WebSocket log gateway.
	Gateway *ws.Gateway

	// Clock is an optional custom clock for testing time-related logic.
	// When nil, the default system clock is used.
	Clock clock.Clock
}

// NewRouter creates a chi.Router using the production route configuration.
// Both the production server and tests should use this function to prevent route drift.
//
// Parameters:
//   - deps: router dependencies
//
// Returns:
//   - chi.Router: the configured router
func NewRouter(deps RouterDeps) chi.Router {
	cfg := deps.Config
	s := &Server{
		Config:  &cfg,
		DB:      deps.DB,
		Redis:   deps.Redis,
		Hub:     deps.Hub,
		Gateway: deps.Gateway,
	}
	var svc *service.Service
	if deps.Clock != nil {
		svc = service.NewWithClock(s.DB, s.Hub, s.Redis, deps.Clock)
	} else {
		svc = service.New(s.DB, s.Hub, s.Redis)
	}
	return s.buildRouter(svc)
}
