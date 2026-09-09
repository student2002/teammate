// service.go implements the core file of the business logic layer, defining the Service struct as the container for all sub-services.
// It provides SSE event publishing capabilities, supporting broadcasting to a workspace and sending to a specific agent.
// It also sends control events (interrupt/rollback/continuation-invite); control events are persisted to the Redis buffer,
// ensuring that control events are not lost after an Agent recovers from being offline.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/clock"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// EventPublisher defines the event publishing interface, used to push SSE events to the Agent runtime.
// Implemented by the infrastructure layer.
type EventPublisher interface {
	Publish(ctx context.Context, subscriberID string, event types.SSEEvent) error
	BufferEvent(ctx context.Context, subscriberID string, event types.SSEEvent) error
}

// sseIDCounter ensures the uniqueness of SSE event IDs under high concurrency.
// Uses an atomically incremented counter combined with a timestamp to generate globally unique IDs.
var sseIDCounter int64

// nextSSEEventID generates a unique SSE event ID, formatted as "{nanosecond timestamp}-{incremented counter}".
func nextSSEEventID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), atomic.AddInt64(&sseIDCounter, 1))
}

// Service holds all data storage and infrastructure connections, providing business logic orchestration capabilities.
// As the unified entry point for all sub-services (AgentService, TaskService, NodeService, etc.),
// it manages the Store, EventPublisher, Redis, and database connections.
type Service struct {
	Store *store.Store   // data access layer, encapsulating sqlc-generated queries
	Hub   EventPublisher // SSE event publisher, managing Agent connections and event broadcasting
	Redis *redis.Client  // Redis client, used for caching, distributed locks, and event buffering
}

// New creates a new Service instance.
//
// Parameters:
//   - pgDB: PostgreSQL connection
//   - hub: SSE event publisher (may be nil, in which case SSE functionality is disabled,
//   - rdb: Redis client (may be nil, in which case caching and buffering functionality is disabled,
//
// Returns:
//   - *Service: the initialized Service instance
func New(pgDB *sql.DB, hub EventPublisher, rdb *redis.Client) *Service {
	return &Service{
		Store: store.New(pgDB),
		Hub:   hub,
		Redis: rdb,
	}
}

// NewWithClock creates a Service instance using a custom clock (for testing).
// The custom clock allows controlling the passage of time in unit tests.
func NewWithClock(pgDB *sql.DB, hub EventPublisher, rdb *redis.Client, c clock.Clock) *Service {
	return &Service{
		Store: store.NewWithClock(pgDB, c),
		Hub:   hub,
		Redis: rdb,
	}
}

// publishToProject broadcasts an SSE event to the agent members of the specified project (project-granularity delivery).
// Difference from workspace broadcasting: it only delivers to the online runtimes of agents that are "members of that project" —
// non-project members do not receive node notifications (the consumers that claim nodes are the project member agents, see
// service/node.go Claim's CheckAgentProjectAccess), preventing unrelated Agents from
// perceiving the existence of tasks within the project.
// If there are no online runtimes, the event is buffered to all runtimes of the member agents (including offline ones),
// ensuring that runtimes can receive missed events when they come back online (SSE reconnection compensation mechanism).
// If Hub is nil (e.g. in a test environment), this is a no-op.
//
// Steps:
//  1. Query the agent members of the project (project_members)
//  2. Serialize the event data to JSON
//  3. For each member agent, query online runtimes and deliver
//  4. If there are no online runtimes, buffer the event to all runtimes (including offline ones) of the member agents for recovery
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//   - eventType: SSE event type (e.g. node:pending)
//   - data: event data (will be JSON-serialized)
func (s *Service) publishToProject(ctx context.Context, projectID uuid.UUID, eventType string, data interface{}) {
	if s.Hub == nil {
		return
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("marshal SSE event data", "event", eventType, "err", err)
		return
	}

	event := types.SSEEvent{
		ID:    nextSSEEventID(),
		Event: eventType,
		Data:  dataBytes,
	}

	members, err := s.Store.ListProjectMembers(ctx, projectID)
	if err != nil {
		slog.Error("list project members for SSE", "project_id", projectID, "err", err)
		return
	}

	onlineSent := false
	for _, m := range members {
		if m.MemberType != "agent" || m.AgentID == nil {
			continue
		}
		agentUUID, err := uuid.Parse(*m.AgentID)
		if err != nil {
			continue
		}
		runtimeIDs, err := s.Store.ListOnlineRuntimeIDsByAgent(ctx, agentUUID)
		if err != nil {
			slog.Error("list online runtimes for agent SSE", "agent_id", m.AgentID, "err", err)
			continue
		}
		for _, rtID := range runtimeIDs {
			if err := s.Hub.Publish(ctx, rtID.String(), event); err != nil {
				slog.Error("publish SSE event to runtime", "runtime_id", rtID, "event", eventType, "err", err)
				continue
			}
			onlineSent = true
		}
	}

	// No online member runtimes: buffer the event to all runtimes (including offline) of the member agents,
	// replayed via Last-Event-ID after offline recovery
	if !onlineSent {
		for _, m := range members {
			if m.MemberType != "agent" || m.AgentID == nil {
				continue
			}
			agentUUID, err := uuid.Parse(*m.AgentID)
			if err != nil {
				continue
			}
			runtimeIDs, err := s.Store.ListRuntimeIDsByAgent(ctx, agentUUID)
			if err != nil {
				continue
			}
			for _, rtID := range runtimeIDs {
				if err := s.Hub.BufferEvent(ctx, rtID.String(), event); err != nil {
					slog.Error("buffer project event for offline runtime", "runtime_id", rtID, "err", err)
				}
			}
		}
	}
}

// publishToAgent sends an SSE event to all online runtimes of the specified agent.
// If Hub is nil (e.g. in a test environment) or there are no online runtimes, this is a no-op.
//
// Steps:
//  1. Query all online runtime IDs of the agent
//  2. If there are no online runtimes, log a warning and drop the event
//  3. Serialize the event data to JSON
//  4. Deliver the event to all online runtimes
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - eventType: SSE event type
//   - data: event data (will be JSON-serialized)
func (s *Service) publishToAgent(ctx context.Context, agentID uuid.UUID, eventType string, data interface{}) {
	if s.Hub == nil {
		return
	}

	runtimeIDs, err := s.Store.ListOnlineRuntimeIDsByAgent(ctx, agentID)
	if err != nil {
		slog.Error("list online runtimes for agent SSE", "agent_id", agentID, "err", err)
		return
	}

	if len(runtimeIDs) == 0 {
		slog.Warn("no online runtimes for agent SSE, event dropped",
			"agent_id", agentID, "event", eventType)
		return
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("marshal SSE event data", "event", eventType, "err", err)
		return
	}

	event := types.SSEEvent{
		ID:    nextSSEEventID(),
		Event: eventType,
		Data:  dataBytes,
	}

	for _, rtID := range runtimeIDs {
		if err := s.Hub.Publish(ctx, rtID.String(), event); err != nil {
			slog.Error("publish SSE event to runtime", "runtime_id", rtID, "event", eventType, "err", err)
		}
	}
}

// publishControlEvent persists and publishes a control event.
// Control events (interrupt/rollback/continuation-invite) must not be lost, and can be recovered even if the target runtime is offline.
// The event is also stored in the SSE event buffer (Redis) and can be retrieved when the runtime comes back online.
//
// Steps:
//  1. Send the event to online runtimes (best-effort delivery)
//  2. Query all runtimes of the agent (including offline ones),
//  3. Buffer the event to Redis for each runtime
//  4. When the runtime reconnects, replay missed events from Redis via Last-Event-ID
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - eventType: control event type (e.g. task:interrupt/node:reject_rollback/node:continuation_invite)
//   - data: event data (will be JSON-serialized)
func (s *Service) PublishControlEvent(ctx context.Context, agentID uuid.UUID, eventType string, data interface{}) {
	s.publishToAgent(ctx, agentID, eventType, data)

	if s.Hub == nil || s.Store == nil {
		return
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("marshal control event data", "event", eventType, "err", err)
		return
	}

	runtimeIDs, err := s.Store.ListRuntimeIDsByAgent(ctx, agentID)
	if err != nil {
		slog.Error("query runtimes for control event buffering", "agent_id", agentID, "err", err)
		return
	}

	event := types.SSEEvent{
		ID:    nextSSEEventID(),
		Event: eventType,
		Data:  dataBytes,
	}

	for _, rtID := range runtimeIDs {
		if err := s.Hub.BufferEvent(ctx, rtID.String(), event); err != nil {
			slog.Error("buffer control event for runtime", "runtime_id", rtID, "event", eventType, "err", err)
		}
	}
}
