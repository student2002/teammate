// hub.go 提供 Server-Sent Events (SSE) Hub，管理事件的发布、订阅和跨实例同步。
// 基于 Redis Pub/Sub 实现多服务器实例间的事件分发，每个运行时（Runtime）有独立的事件频道。
// 支持事件缓冲和重放：事件存储到 Redis 有序集合中，Agent 重连时通过 Last-Event-ID 恢复丢失事件。
//
// SSE 事件类型：
//   - node:pending — 新节点待认领
//   - node:continuation_invite — 节点完成后续约权邀请
//   - mention:trigger — @提及触发
//   - task:interrupt — 任务中断（控制事件，保证送达）
//   - node:timeout — 节点超时（控制事件）
//   - node:reject_rollback — 审查拒绝回滚（控制事件）
//   - sync:required — 需要全量同步（控制事件）
//   - permission:changed — 权限变更（控制事件）
//
// 控制事件（interrupt/rollback/timeout/sync/permission）使用带超时的阻塞发送确保送达。
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
	// BufferTTL 是事件在 Redis 有序集合缓冲区中的保留时间（1 小时）。
	BufferTTL = 1 * time.Hour
	// BufferKeyPrefix 是 Redis 中 SSE 事件缓冲区的键前缀，格式为 "sse_buffer:{runtimeID}"。
	BufferKeyPrefix = "sse_buffer:"
)

// 标准 SSE 事件类型常量，从 types 包 re-export 以保持向后兼容。
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

// IsControlEvent 判断指定事件类型是否为控制事件（需要保证送达的高优先级事件）。
// 控制事件使用带超时的阻塞发送，非控制事件使用尽力投递（通道满时丢弃）。
//
// 参数：
//   - eventType: 事件类型字符串
//
// 返回：
//   - bool: 是否为控制事件
func IsControlEvent(eventType string) bool {
	switch eventType {
	case EventTaskInterrupt, EventNodeRejectRollback, EventNodeTimeout, EventSyncRequired, EventPermissionChanged:
		return true
	default:
		return false
	}
}

// SSEEvent 是 types.SSEEvent 的别名，保持向后兼容。
type SSEEvent = types.SSEEvent

// Hub 是 SSE 事件中心，管理事件的发布、订阅和跨实例同步。
// 每个服务器实例维护本地订阅者列表，通过 Redis Pub/Sub 实现跨实例事件分发。
type Hub struct {
	redis   *redis.Client
	mu      sync.RWMutex
	clients map[string][]chan SSEEvent // runtime_id -> 订阅通道列表
}

// NewHub 创建一个新的 Hub 实例，使用给定的 Redis 客户端。
//
// 参数：
//   - rdb: Redis 客户端，用于 Pub/Sub 和事件缓冲
//
// 返回：
//   - *Hub: 初始化后的 Hub 实例
func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		redis:   rdb,
		clients: make(map[string][]chan SSEEvent),
	}
}

// RedisChannel 返回指定运行时的 Redis Pub/Sub 频道名，格式为 "sse:{runtimeID}"。
//
// 参数：
//   - runtimeID: 运行时的唯一标识符
//
// 返回：
//   - string: Redis 频道名
func RedisChannel(runtimeID string) string {
	return fmt.Sprintf("sse:%s", runtimeID)
}

// BufferKey 返回指定运行时的 Redis 缓冲区键名，格式为 "sse_buffer:{runtimeID}"。
//
// 参数：
//   - runtimeID: 运行时的唯一标识符
//
// 返回：
//   - string: Redis 缓冲区键名
func BufferKey(runtimeID string) string {
	return BufferKeyPrefix + runtimeID
}

// BufferEvent 将事件存储到 Redis 有序集合缓冲区中，用于后续重放。
// 事件 ID（Unix 纳秒时间戳字符串）作为分数，支持 Last-Event-ID 重连的高效范围查询。
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: 运行时的唯一标识符
//   - event: 要缓冲的 SSE 事件
//
// 返回：
//   - error: Redis 写入失败时返回错误
func (h *Hub) BufferEvent(ctx context.Context, runtimeID string, event SSEEvent) error {
	if h.redis == nil {
		return nil
	}
	key := BufferKey(runtimeID)

	// 将事件 ID 解析为分数，事件 ID 是 Unix 纳秒时间戳
	score, err := strconv.ParseFloat(event.ID, 64)
	if err != nil {
		// 降级：如果 ID 不是时间戳格式，使用当前时间
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

// GetBufferedEvents 获取缓冲区中 ID 大于 afterEventID 的所有事件，按 ID 升序返回。
// 用于 Agent 重连时恢复丢失的事件（Last-Event-ID 机制）。
//
// 重连流程：
//  1. Agent 重连时发送 Last-Event-ID 头
//  2. Server 查询缓冲区中大于该 ID 的所有事件
//  3. 将缓冲事件按顺序发送给 Agent
//  4. 如果缓冲区为空或已过期，推送 sync:required 事件触发全量同步
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: 运行时的唯一标识符
//   - afterEventID: 上次接收的事件 ID（Unix 纳秒时间戳字符串），为空则返回 nil
//
// 返回：
//   - []SSEEvent: 缓冲的事件列表（按 ID 升序）
//   - error: Redis 查询失败时返回错误
func (h *Hub) GetBufferedEvents(ctx context.Context, runtimeID string, afterEventID string) ([]SSEEvent, error) {
	if afterEventID == "" || h.redis == nil {
		return nil, nil
	}

	key := BufferKey(runtimeID)

	// 将 afterEventID 解析为最小分数（开区间）
	minScore, err := strconv.ParseFloat(afterEventID, 64)
	if err != nil {
		// 无法解析 ID 格式 — 返回空以触发全量同步
		return nil, nil
	}

	// ZRANGEBYSCORE 开区间查询 (minScore, +inf)
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

// Subscribe 订阅指定运行时的 SSE 事件，返回接收通道和取消订阅函数。
// 客户端断开连接时必须调用取消订阅函数以释放资源。
//
// 参数：
//   - runtimeID: 要订阅的运行时 ID
//
// 返回：
//   - <-chan SSEEvent: SSE 事件接收通道（缓冲大小 64）
//   - func(): 取消订阅函数，客户端断开时必须调用
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
		// 使用 recover 防止关闭时的重复关闭 panic
		func() {
			defer func() { recover() }()
			close(ch)
		}()
	}

	return ch, unsub
}

// Publish 将事件投递给本地客户端，缓冲到 Redis 用于重放，并发布到 Redis Pub/Sub 供其他实例投递。
// 控制事件（interrupt/rollback/timeout 等）使用带超时的阻塞发送确保送达。
//
// 处理流程：
//  1. 缓冲事件到 Redis 有序集合（用于 Last-Event-ID 重连）
//  2. 投递给本地订阅者：
//     - 控制事件：带 2 秒超时的阻塞发送，确保送达
//     - 非控制事件：非阻塞发送，通道满时丢弃并记录警告日志
//  3. 发布到 Redis Pub/Sub 频道（供其他实例投递）
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: 事件目标运行时的 ID
//   - event: 要发布的 SSE 事件
//
// 返回：
//   - error: Redis 发布失败时返回错误
func (h *Hub) Publish(ctx context.Context, runtimeID string, event SSEEvent) error {
	// 缓冲事件用于 Last-Event-ID 重连
	if err := h.BufferEvent(ctx, runtimeID, event); err != nil {
		// 记录日志但不失败 — 缓冲是尽力而为的
		slog.Error("buffer SSE event", "runtime_id", runtimeID, "event_id", event.ID, "err", err)
	}

	// 投递给本地客户端
	h.mu.RLock()
	subs := h.clients[runtimeID]
	// 复制切片以避免在发送时持有锁
	localSubs := make([]chan SSEEvent, len(subs))
	copy(localSubs, subs)
	h.mu.RUnlock()

	for _, ch := range localSubs {
		if IsControlEvent(event.Event) {
			// 控制事件：带超时的阻塞发送，确保送达
			select {
			case ch <- event:
			case <-time.After(2 * time.Second):
				slog.Error("SSE control event delivery timeout, channel full",
					"runtime_id", runtimeID, "event_id", event.ID, "event_type", event.Event)
			}
		} else {
			// 非控制事件：尽力投递
			select {
			case ch <- event:
			default:
				slog.Warn("SSE client channel full, dropping event", "runtime_id", runtimeID, "event_id", event.ID)
			}
		}
	}

	// 发布到 Redis 供跨实例投递
	if h.redis == nil {
		return nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal SSE event: %w", err)
	}
	return h.redis.Publish(ctx, RedisChannel(runtimeID), data).Err()
}

// Start 开始监听 Redis Pub/Sub 消息并分发给本地订阅者。
// 使用模式订阅接收所有运行时的消息，阻塞直到 ctx 被取消。
//
// 工作流程：
//  1. 使用 PSubscribe 订阅所有 "sse:*" 频道
//  2. 接收消息后反序列化为 SSEEvent
//  3. 从频道名提取 runtimeID
//  4. 分发给对应的本地订阅者
//
// 参数：
//   - ctx: 上下文，取消后停止监听
func (h *Hub) Start(ctx context.Context) {
	// 使用模式订阅，接收所有运行时的消息
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

			// 从频道名 "sse:{runtimeID}" 中提取 runtimeID
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

// ClientCount 返回指定运行时的订阅者数量。
//
// 参数：
//   - runtimeID: 运行时的唯一标识符
//
// 返回：
//   - int: 当前订阅者数量
func (h *Hub) ClientCount(runtimeID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[runtimeID])
}

// Close 关闭所有客户端通道，清理资源。在服务器关闭时调用。
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
