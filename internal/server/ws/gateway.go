// gateway.go provides a WebSocket log gateway that manages log publishing, subscription, and cross-instance synchronization.
// It uses Redis Pub/Sub for log distribution across multiple server instances; each task has an independent log channel.
// It supports log history queries: log messages are buffered into a Redis sorted set and retained for 2 hours.
//
// Security feature: all log messages are automatically masked before publishing (see desensitize.go).
// Event types: log types include "stdout" (standard output), "stderr" (standard error), and "system" (system message).
package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// LogMessage represents a single log message pushed to WebSocket clients.
type LogMessage struct {
	// TaskID is the unique identifier of the task the log belongs to.
	TaskID string `json:"task_id"`
	// NodeID is the unique identifier of the workflow node the log belongs to.
	NodeID string `json:"node_id"`
	// Type is the log type: "stdout" (standard output), "stderr" (standard error), "system" (system message).
	Type string `json:"type"` // "stdout", "stderr", "system"
	// Content is the log content (already masked).
	Content string `json:"content"`
	// Timestamp is the Unix millisecond timestamp when the log was generated.
	Timestamp int64 `json:"timestamp"`
}

const (
	// LogBufferTTL is the retention time of log messages in the Redis sorted-set buffer (2 hours).
	LogBufferTTL = 2 * time.Hour
	// LogBufferKeyPrefix is the Redis key prefix for the log buffer, formatted as "log_buffer:{taskID}".
	LogBufferKeyPrefix = "log_buffer:"
)

// Gateway is the WebSocket log gateway, managing log publishing, subscription, and cross-instance synchronization.
// Each server instance maintains a local subscriber list and uses Redis Pub/Sub for cross-instance log distribution.
type Gateway struct {
	redis   *redis.Client
	mu      sync.RWMutex
	clients map[string][]chan LogMessage // task_id -> list of subscribe channels
	id      string                       // unique instance ID, used to avoid self-delivery (skip this instance's messages on publish→subscribe)
}

// NewGateway creates a new Gateway instance using the given Redis client.
//
// Parameters:
//   - rdb: Redis client, used for Pub/Sub and log buffering
//
// Returns:
//   - *Gateway: the initialized gateway instance
func NewGateway(rdb *redis.Client) *Gateway {
	return &Gateway{
		redis:   rdb,
		clients: make(map[string][]chan LogMessage),
		id:      uuid.New().String(),
	}
}

// LogChannel returns the Redis Pub/Sub channel name for the specified task, formatted as "logs:{taskID}".
//
// Parameters:
//   - taskID: the unique identifier of the task
//
// Returns:
//   - string: the Redis channel name
func LogChannel(taskID string) string {
	return fmt.Sprintf("logs:%s", taskID)
}

// Subscribe subscribes to log messages for the specified task, returning a receive channel and an unsubscribe function.
// The unsubscribe function must be called when the client disconnects to release resources.
//
// Parameters:
//   - taskID: the task ID to subscribe to
//
// Returns:
//   - <-chan LogMessage: the log message receive channel (buffer size 64)
//   - func(): the unsubscribe function, must be called when the client disconnects
func (g *Gateway) Subscribe(taskID string) (<-chan LogMessage, func()) {
	ch := make(chan LogMessage, 64)

	g.mu.Lock()
	g.clients[taskID] = append(g.clients[taskID], ch)
	g.mu.Unlock()

	unsub := func() {
		g.mu.Lock()
		defer g.mu.Unlock()

		subs := g.clients[taskID]
		for i, s := range subs {
			if s == ch {
				g.clients[taskID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(g.clients[taskID]) == 0 {
			delete(g.clients, taskID)
		}
		close(ch)
	}

	return ch, unsub
}

// redisLogMessage is the struct that wraps a LogMessage during Redis transport, including the source instance ID for deduplication.
type redisLogMessage struct {
	// Source is the UUID of the server instance that sent the message; the receiver uses it to skip self-delivered messages.
	Source string     `json:"source"`
	// Msg is the actual log message content.
	Msg LogMessage `json:"msg"`
}

// PublishLog masks the message content, delivers it to local subscribers, and publishes it to Redis for delivery by other server instances.
//
// Processing flow:
//  1. Mask the message content (API Key, Token, password, etc.)
//  2. Set the timestamp and task ID
//  3. Deliver to local subscribers (non-blocking; messages are dropped when the channel is full)
//  4. Buffer into a Redis sorted set (for history queries)
//  5. Publish to the Redis Pub/Sub channel (for delivery by other instances)
//
// Parameters:
//   - ctx: request context
//   - taskID: the ID of the task the log belongs to
//   - msg: the log message (Content will be masked)
//
// Returns:
//   - error: returned when Redis publishing fails
func (g *Gateway) PublishLog(ctx context.Context, taskID string, msg LogMessage) error {
	// Mask the content before publishing
	msg.Content = Desensitize(msg.Content)

	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}
	msg.TaskID = taskID

	// Deliver to local clients
	g.mu.RLock()
	subs := g.clients[taskID]
	localSubs := make([]chan LogMessage, len(subs))
	copy(localSubs, subs)
	g.mu.RUnlock()

	for _, ch := range localSubs {
		select {
		case ch <- msg:
		default:
			slog.Warn("log client channel full, dropping message", "task_id", taskID)
		}
	}

	// Publish to Redis for cross-instance delivery (includes the source instance ID for deduplication)
	if g.redis == nil {
		return nil
	}
	wrapped := redisLogMessage{Source: g.id, Msg: msg}
	data, err := json.Marshal(wrapped)
	if err != nil {
		return fmt.Errorf("marshal log message: %w", err)
	}

	// Buffer the message into a Redis sorted set for history queries
	bufKey := LogBufferKeyPrefix + taskID
	bufData, _ := json.Marshal(msg)
	pipe := g.redis.Pipeline()
	pipe.ZAdd(ctx, bufKey, redis.Z{Score: float64(msg.Timestamp), Member: bufData})
	pipe.Expire(ctx, bufKey, LogBufferTTL)
	if _, bufErr := pipe.Exec(ctx); bufErr != nil {
		slog.Error("buffer log message", "task_id", taskID, "err", bufErr)
	}

	return g.redis.Publish(ctx, LogChannel(taskID), data).Err()
}

// Start begins listening for "logs:*" pattern messages on Redis Pub/Sub and distributes logs to local subscribers.
// It blocks until ctx is canceled. Messages from this instance are skipped to avoid duplicate delivery.
//
// Workflow:
//  1. Use PSubscribe to subscribe to all "logs:*" channels
//  2. After receiving a message, deserialize it into a redisLogMessage
//  3. Skip messages from this instance (matched by the Source field)
//  4. Extract the taskID from the channel name and dispatch to the corresponding local subscribers
//
// Parameters:
//   - ctx: context; listening stops when canceled
func (g *Gateway) Start(ctx context.Context) {
	sub := g.redis.PSubscribe(ctx, "logs:*")
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
			var wrapped redisLogMessage
			if err := json.Unmarshal([]byte(msg.Payload), &wrapped); err != nil {
				slog.Error("unmarshal log message from Redis", "err", err)
				continue
			}

			// Skip this instance's messages — already delivered locally
			if wrapped.Source == g.id {
				continue
			}

			// Extract the taskID from the channel name "logs:{taskID}"
			taskID := strings.TrimPrefix(msg.Channel, "logs:")

			g.mu.RLock()
			subs := g.clients[taskID]
			localSubs := make([]chan LogMessage, len(subs))
			copy(localSubs, subs)
			g.mu.RUnlock()

			for _, c := range localSubs {
				select {
				case c <- wrapped.Msg:
				default:
					slog.Warn("log client channel full, dropping message", "task_id", taskID)
				}
			}
		}
	}
}

// GetBufferedLogs retrieves all buffered log messages for the specified task from Redis, returned in ascending timestamp order.
// Used to restore historical logs after a client reconnects.
//
// Parameters:
//   - ctx: request context
//   - taskID: the unique identifier of the task
//
// Returns:
//   - []LogMessage: the list of buffered log messages (in ascending timestamp order)
//   - error: returned when the Redis query fails
func (g *Gateway) GetBufferedLogs(ctx context.Context, taskID string) ([]LogMessage, error) {
	if g.redis == nil {
		return nil, nil
	}
	key := LogBufferKeyPrefix + taskID
	results, err := g.redis.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: "-inf",
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("redis ZRANGEBYSCORE: %w", err)
	}

	msgs := make([]LogMessage, 0, len(results))
	for _, raw := range results {
		var msg LogMessage
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			slog.Error("unmarshal buffered log message", "err", err)
			continue
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// ClientCount returns the number of subscribers for the specified task.
//
// Parameters:
//   - taskID: the unique identifier of the task
//
// Returns:
//   - int: the current number of subscribers
func (g *Gateway) ClientCount(taskID string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.clients[taskID])
}

// Close closes all client channels and releases resources. Called when the server shuts down.
func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for taskID, subs := range g.clients {
		for _, ch := range subs {
			close(ch)
		}
		delete(g.clients, taskID)
	}
}
