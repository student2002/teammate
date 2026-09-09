// hub.go provides the Server-Sent Events (SSE) Hub, managing event publishing, subscription, and cross-instance synchronization.
// It uses Redis Pub/Sub for event distribution across multiple server instances; each runtime (Runtime) has an independent event channel.
// It supports event buffering and replay: events are stored in a Redis sorted set, and on agent reconnect, lost events are recovered via Last-Event-ID.
//
// SSE event types:
//   - node:pending — a new node pending claim
//   - node:continuation_invite — a node completed; continuation-invitation
//   - mention:trigger — @mention triggered
//   - task:interrupt — task interrupt (control event, guaranteed delivery)
//   - node:timeout — node timeout (control event)
//   - node:reject_rollback — review reject rollback (control event)
//   - sync:required — full sync required (control event)
//   - permission:changed — permission changed (control event)
//
// Control events (interrupt/rollback/timeout/sync/permission) use a blocking send with a timeout to guarantee delivery.
package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/types"
)

const (
	// BufferTTL is the retention time of events in the Redis sorted-set buffer (1 hour).
	BufferTTL = 1 * time.Hour
	// BufferKeyPrefix is the Redis key prefix for the SSE event buffer, formatted as "sse_buffer:{runtimeID}".
	BufferKeyPrefix = "sse_buffer:"
)

// Standard SSE event type constants, re-exported from the types package for backward compatibility.
const (
	EventNodePending            = types.EventNodePending
	EventNodeContinuationInvite = types.EventNodeContinuationInvite
	EventMentionTrigger         = types.EventMentionTrigger
	EventTaskInterrupt          = types.EventTaskInterrupt
	EventNodeTimeout            = types.EventNodeTimeout
	EventSyncRequired           = types.EventSyncRequired
	EventNodeRejectRollback     = types.EventNodeRejectRollback
	EventPermissionChanged      = types.EventPermissionChanged
)

// IsControlEvent determines whether the specified event type is a control event (a high-priority event that requires guaranteed delivery).
// Control events use a blocking send with a timeout; non-control events use best-effort delivery (dropped when the channel is full).
//
// Parameters:
//   - eventType: the event type string
//
// Returns:
//   - bool: whether it is a control event
func IsControlEvent(eventType string) bool {
	switch eventType {
	case EventTaskInterrupt, EventNodeRejectRollback, EventNodeTimeout, EventSyncRequired, EventPermissionChanged:
		return true
	default:
		return false
	}
}

// SSEEvent is an alias for types.SSEEvent, for backward compatibility.
type SSEEvent = types.SSEEvent

// Hub is the SSE event center, managing event publishing, subscription, and cross-instance synchronization.
// Each server instance maintains a local subscriber list and uses Redis Pub/Sub for cross-instance event distribution.
type Hub struct {
	redis   *redis.Client
	mu      sync.RWMutex
	clients map[string][]chan SSEEvent // runtime_id -> list of subscribe channels
}

// NewHub creates a new Hub instance using the given Redis client.
//
// Parameters:
//   - rdb: Redis client, used for Pub/Sub and event buffering
//
// Returns:
//   - *Hub: the initialized Hub instance
func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		redis:   rdb,
		clients: make(map[string][]chan SSEEvent),
	}
}

// RedisChannel returns the Redis Pub/Sub channel name for the specified runtime, formatted as "sse:{runtimeID}".
//
// Parameters:
//   - runtimeID: the unique identifier of the runtime
//
// Returns:
//   - string: the Redis channel name
func RedisChannel(runtimeID string) string {
	return fmt.Sprintf("sse:%s", runtimeID)
}

// BufferKey returns the Redis buffer key name for the specified runtime, formatted as "sse_buffer:{runtimeID}".
//
// Parameters:
//   - runtimeID: the unique identifier of the runtime
//
// Returns:
//   - string: the Redis buffer key name
func BufferKey(runtimeID string) string {
	return BufferKeyPrefix + runtimeID
}

// BufferEvent stores an event into the Redis sorted-set buffer for later replay.
// The event ID (a Unix nanosecond timestamp string) is used as the score, supporting efficient range queries for Last-Event-ID reconnection.
//
// Parameters:
//   - ctx: request context
//   - runtimeID: the unique identifier of the runtime
//   - event: the SSE event to buffer
//
// Returns:
//   - error: returned when the Redis write fails
func (h *Hub) BufferEvent(ctx context.Context, runtimeID string, event SSEEvent) error {
	if h.redis == nil {
		return nil
	}
	key := BufferKey(runtimeID)

	// Parse the event ID as the score; the event ID is a Unix nanosecond timestamp
	score, err := strconv.ParseFloat(event.ID, 64)
	if err != nil {
		// Fallback: if the ID is not in timestamp format, use the current time
		score = float64(time.Now().UnixNano())
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal SSE event for buffer: %w", err)
	}

	pipe := h.redis.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: data})
	pipe.Expire(ctx, key, BufferTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis pipeline buffer event: %w", err)
	}
	return nil
}

// GetBufferedEvents retrieves all events in the buffer with an ID greater than afterEventID, returned in ascending ID order.
// Used to recover lost events when an agent reconnects (Last-Event-ID mechanism).
//
// Reconnection flow:
//  1. On reconnect the agent sends the Last-Event-ID header
//  2. The server queries all events in the buffer with an ID greater than it
//  3. The buffered events are sent to the agent in order
//  4. If the buffer is empty or expired, a sync:required event is pushed to trigger a full sync
//
// Parameters:
//   - ctx: request context
//   - runtimeID: the unique identifier of the runtime
//   - afterEventID: the ID of the last received event (a Unix nanosecond timestamp string); if empty, returns nil
//
// Returns:
//   - []SSEEvent: the list of buffered events (in ascending ID order)
//   - error: returned when the Redis query fails
func (h *Hub) GetBufferedEvents(ctx context.Context, runtimeID string, afterEventID string) ([]SSEEvent, error) {
	if afterEventID == "" || h.redis == nil {
		return nil, nil
	}

	key := BufferKey(runtimeID)

	// Parse afterEventID as the minimum score (open interval)
	minScore, err := strconv.ParseFloat(afterEventID, 64)
	if err != nil {
		// Cannot parse the ID format — return empty to trigger a full sync
		return nil, nil
	}

	// ZRANGEBYSCORE open-interval query (minScore, +inf)
	results, err := h.redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: fmt.Sprintf("(%f", minScore),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("redis ZRANGEBYSCORE: %w", err)
	}

	events := make([]SSEEvent, 0, len(results))
	for _, raw := range results {
		var event SSEEvent
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			slog.Error("unmarshal buffered SSE event", "err", err)
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

// Subscribe subscribes to SSE events for the specified runtime, returning a receive channel and an unsubscribe function.
// The unsubscribe function must be called when the client disconnects to release resources.
//
// Parameters:
//   - runtimeID: the runtime ID to subscribe to
//
// Returns:
//   - <-chan SSEEvent: the SSE event receive channel (buffer size 64)
//   - func(): the unsubscribe function, must be called when the client disconnects
func (h *Hub) Subscribe(runtimeID string) (<-chan SSEEvent, func()) {
	ch := make(chan SSEEvent, 64)

	h.mu.Lock()
	h.clients[runtimeID] = append(h.clients[runtimeID], ch)
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		subs := h.clients[runtimeID]
		for i, s := range subs {
			if s == ch {
				h.clients[runtimeID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(h.clients[runtimeID]) == 0 {
			delete(h.clients, runtimeID)
		}
		// Use recover to prevent a double-close panic during closing
		func() {
			defer func() { recover() }()
			close(ch)
		}()
	}

	return ch, unsub
}

// Publish delivers the event to local clients, buffers it to Redis for replay, and publishes it to Redis Pub/Sub for delivery by other instances.
// Control events (interrupt/rollback/timeout, etc.) use a blocking send with a timeout to guarantee delivery.
//
// Processing flow:
//  1. Buffer the event into a Redis sorted set (for Last-Event-ID reconnection)
//  2. Deliver to local subscribers:
//     - Control events: blocking send with a 2-second timeout, guaranteeing delivery
//     - Non-control events: non-blocking send, dropped and logged as a warning when the channel is full
//  3. Publish to the Redis Pub/Sub channel (for delivery by other instances)
//
// Parameters:
//   - ctx: request context
//   - runtimeID: the ID of the runtime the event targets
//   - event: the SSE event to publish
//
// Returns:
//   - error: returned when Redis publishing fails
func (h *Hub) Publish(ctx context.Context, runtimeID string, event SSEEvent) error {
	// Buffer the event for Last-Event-ID reconnection
	if err := h.BufferEvent(ctx, runtimeID, event); err != nil {
		// Log but do not fail — buffering is best-effort
		slog.Error("buffer SSE event", "runtime_id", runtimeID, "event_id", event.ID, "err", err)
	}

	// Deliver to local clients
	h.mu.RLock()
	subs := h.clients[runtimeID]
	// Copy the slice to avoid holding the lock while sending
	localSubs := make([]chan SSEEvent, len(subs))
	copy(localSubs, subs)
	h.mu.RUnlock()

	for _, ch := range localSubs {
		if IsControlEvent(event.Event) {
			// Control event: blocking send with a timeout, guaranteeing delivery
			select {
			case ch <- event:
			case <-time.After(2 * time.Second):
				slog.Error("SSE control event delivery timeout, channel full",
					"runtime_id", runtimeID, "event_id", event.ID, "event_type", event.Event)
			}
		} else {
			// Non-control event: best-effort delivery
			select {
			case ch <- event:
			default:
				slog.Warn("SSE client channel full, dropping event", "runtime_id", runtimeID, "event_id", event.ID)
			}
		}
	}

	// Publish to Redis for cross-instance delivery
	if h.redis == nil {
		return nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal SSE event: %w", err)
	}
	return h.redis.Publish(ctx, RedisChannel(runtimeID), data).Err()
}

// Start begins listening for Redis Pub/Sub messages and dispatching them to local subscribers.
// It uses pattern subscription to receive messages for all runtimes, and blocks until ctx is canceled.
//
// Workflow:
//  1. Use PSubscribe to subscribe to all "sse:*" channels
//  2. After receiving a message, deserialize it into an SSEEvent
//  3. Extract the runtimeID from the channel name
//  4. Dispatch to the corresponding local subscribers
//
// Parameters:
//   - ctx: context; listening stops when canceled
func (h *Hub) Start(ctx context.Context) {
	// Use pattern subscription to receive messages for all runtimes
	sub := h.redis.PSubscribe(ctx, "sse:*")
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var event SSEEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				slog.Error("unmarshal SSE event from Redis", "err", err)
				continue
			}

			// Extract the runtimeID from the channel name "sse:{runtimeID}"
			runtimeID := msg.Channel[len("sse:"):]

			h.mu.RLock()
			subs := h.clients[runtimeID]
			localSubs := make([]chan SSEEvent, len(subs))
			copy(localSubs, subs)
			h.mu.RUnlock()

			for _, c := range localSubs {
				select {
				case c <- event:
				default:
					slog.Warn("SSE client channel full, dropping event", "runtime_id", runtimeID, "event_id", event.ID)
				}
			}
		}
	}
}

// ClientCount returns the number of subscribers for the specified runtime.
//
// Parameters:
//   - runtimeID: the unique identifier of the runtime
//
// Returns:
//   - int: the current number of subscribers
func (h *Hub) ClientCount(runtimeID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[runtimeID])
}

// Close closes all client channels and releases resources. Called when the server shuts down.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for runtimeID, subs := range h.clients {
		for _, ch := range subs {
			close(ch)
		}
		delete(h.clients, runtimeID)
	}
}
