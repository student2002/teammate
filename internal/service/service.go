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
	"github.com/teammate/server/internal/workflow"
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
// As the unified entry point for all sub-services (AgentService, TaskService, WorkflowService, etc.),
// it manages the Store, EventPublisher, Redis, and database connections.
type Service struct {
	Store *store.Store   // data access layer, encapsulating sqlc-generated queries
	Hub   EventPublisher // SSE event publisher, managing Agent connections and event broadcasting
	Redis *redis.Client  // Redis client, used for caching, distributed locks, and event buffering
	// WorkflowRegistry holds the V2 node catalog; it is the trust boundary for
	// template graph validation and node config checks.
	WorkflowRegistry *workflow.Registry
	// GitHubAPIURL is the GitHub REST API root used by the GitHub client
	// (TEAMS_GITHUB_API_URL). Empty selects the client's public default.
	GitHubAPIURL string
	// HTTPNodeAllowHTTP permits action.http nodes to target plain http:// URLs
	// (TEAMS_HTTP_NODE_ALLOW_HTTP). Default off: only https:// is accepted.
	HTTPNodeAllowHTTP bool
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
		Store:            store.New(pgDB),
		Hub:              hub,
		Redis:            rdb,
		WorkflowRegistry: mustBuiltinWorkflowRegistry(),
	}
}

// NewWithClock creates a Service instance using a custom clock (for testing).
// The custom clock allows controlling the passage of time in unit tests.
func NewWithClock(pgDB *sql.DB, hub EventPublisher, rdb *redis.Client, c clock.Clock) *Service {
	return &Service{
		Store:            store.NewWithClock(pgDB, c),
		Hub:              hub,
		Redis:            rdb,
		WorkflowRegistry: mustBuiltinWorkflowRegistry(),
	}
}

// publishToProject broadcasts an SSE event to the agent members of the specified project (project-granularity delivery).
// Difference from workspace broadcasting: it only delivers to the online runtimes of agents that are "members of that project" —
// non-project members do not receive node notifications (the consumers that claim nodes are the project member agents),
// preventing unrelated Agents from perceiving the existence of tasks within the project.
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
		if s.publishToAgent(ctx, agentUUID, eventType, data) {
			onlineSent = true
		}
	}
	_ = onlineSent
}

// daemonSubscriberKey is the Hub subscribe key of a daemon's single SSE stream.
func daemonSubscriberKey(daemonID uuid.UUID) string {
	return "daemon:" + daemonID.String()
}

// withAgentEnvelope merges agent_id into the event payload so the daemon can
// route the event to the local runtime of that agent. Non-object payloads are
// wrapped under "payload".
func withAgentEnvelope(agentID uuid.UUID, dataBytes []byte) []byte {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(dataBytes, &payload); err != nil || payload == nil {
		payload = map[string]json.RawMessage{"payload": json.RawMessage(dataBytes)}
	}
	payload["agent_id"] = json.RawMessage(`"` + agentID.String() + `"`)
	out, err := json.Marshal(payload)
	if err != nil {
		return dataBytes
	}
	return out
}

// publishToAgent sends an SSE event on the daemon stream of the specified agent.
// The daemon must be online; the event envelope carries agent_id for local routing.
// Returns whether the event was delivered to an online daemon.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - eventType: SSE event type
//   - data: event data (will be JSON-serialized)
func (s *Service) publishToAgent(ctx context.Context, agentID uuid.UUID, eventType string, data interface{}) bool {
	if s.Hub == nil {
		return false
	}

	daemonID, online, err := s.Store.GetOnlineDaemonIDByAgent(ctx, agentID)
	if err != nil || !online {
		slog.Warn("agent daemon offline or unmanaged, event dropped",
			"agent_id", agentID, "event", eventType)
		return false
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("marshal SSE event data", "event", eventType, "err", err)
		return false
	}

	event := types.SSEEvent{
		ID:    nextSSEEventID(),
		Event: eventType,
		Data:  withAgentEnvelope(agentID, dataBytes),
	}

	if err := s.Hub.Publish(ctx, daemonSubscriberKey(daemonID), event); err != nil {
		slog.Error("publish SSE event to daemon", "daemon_id", daemonID, "event", eventType, "err", err)
		return false
	}
	return true
}

// PublishControlEvent persists and publishes a control event.
// Control events (interrupt/rollback/continuation-invite) must not be lost and
// are buffered on the daemon stream, replayed via Last-Event-ID after reconnect.
//
// The daemon is resolved regardless of online state and the event goes through
// a single Hub.Publish: online daemons get the live delivery, offline ones get
// exactly one buffered copy replayed on reconnect.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - eventType: control event type (e.g. task:interrupt/node:reject_rollback/node:continuation_invite)
//   - data: event data (will be JSON-serialized)
func (s *Service) PublishControlEvent(ctx context.Context, agentID uuid.UUID, eventType string, data interface{}) {
	if s.Hub == nil || s.Store == nil {
		return
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		slog.Error("marshal control event data", "event", eventType, "err", err)
		return
	}

	daemonID, ok, err := s.Store.GetDaemonIDByAgent(ctx, agentID)
	if err != nil || !ok {
		return
	}

	event := types.SSEEvent{
		ID:    nextSSEEventID(),
		Event: eventType,
		Data:  withAgentEnvelope(agentID, dataBytes),
	}

	if err := s.Hub.Publish(ctx, daemonSubscriberKey(daemonID), event); err != nil {
		slog.Error("publish control event to daemon", "daemon_id", daemonID, "event", eventType, "err", err)
	}
}
