// gateway.go 提供 WebSocket 日志网关，管理日志的发布、订阅和跨实例同步。
// 基于 Redis Pub/Sub 实现多服务器实例间的日志分发，每个任务有独立的日志频道。
// 支持日志历史查询：日志消息缓冲到 Redis 有序集合中，保留 2 小时。
// 安全特性：所有日志消息在发布前自动进行脱敏处理（见 desensitize.go）。
// 事件类型：日志类型包括 "stdout"（标准输出）、"stderr"（标准错误）、"system"（系统消息）。
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

// LogMessage 表示推送给 WebSocket 客户端的单条日志消息。
type LogMessage struct {
	// TaskID 是日志所属任务的唯一标识符。
	TaskID string `json:"task_id"`
	// NodeID 是日志所属工作流节点的唯一标识符。
	NodeID string `json:"node_id"`
	// Type 是日志类型："stdout"（标准输出）、"stderr"（标准错误）、"system"（系统消息）。
	Type string `json:"type"` // "stdout"、"stderr"、"system"
	// Content 是日志内容（已脱敏处理）。
	Content string `json:"content"`
	// Timestamp 是日志生成的 Unix 毫秒时间戳。
	Timestamp int64 `json:"timestamp"`
}

const (
	// LogBufferTTL 是日志消息在 Redis 有序集合缓冲区中的保留时间（2 小时）。
	LogBufferTTL = 2 * time.Hour
	// LogBufferKeyPrefix 是 Redis 中日志缓冲区的键前缀，格式为 "log_buffer:{taskID}"。
	LogBufferKeyPrefix = "log_buffer:"
)

// Gateway 是 WebSocket 日志网关，管理日志的发布、订阅和跨实例同步。
// 每个服务器实例维护本地订阅者列表，通过 Redis Pub/Sub 实现跨实例日志分发。
type Gateway struct {
	redis   *redis.Client
	mu      sync.RWMutex
	clients map[string][]chan LogMessage // task_id -> 订阅通道列表
	id      string                       // 唯一实例 ID，用于避免自我投递（发布→订阅时跳过本实例的消息）
}

// NewGateway 创建一个新的 Gateway 实例，使用给定的 Redis 客户端。
//
// 参数：
//   - rdb: Redis 客户端，用于 Pub/Sub 和日志缓冲
//
// 返回：
//   - *Gateway: 初始化后的网关实例
func NewGateway(rdb *redis.Client) *Gateway {
	return &Gateway{
		redis:   rdb,
		clients: make(map[string][]chan LogMessage),
		id:      uuid.New().String(),
	}
}

// LogChannel 返回指定任务的 Redis Pub/Sub 频道名，格式为 "logs:{taskID}"。
//
// 参数：
//   - taskID: 任务的唯一标识符
//
// 返回：
//   - string: Redis 频道名
func LogChannel(taskID string) string {
	return fmt.Sprintf("logs:%s", taskID)
}

// Subscribe 订阅指定任务的日志消息，返回接收通道和取消订阅函数。
// 客户端断开连接时必须调用取消订阅函数以释放资源。
//
// 参数：
//   - taskID: 要订阅的任务 ID
//
// 返回：
//   - <-chan LogMessage: 日志消息接收通道（缓冲大小 64）
//   - func(): 取消订阅函数，客户端断开时必须调用
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

// redisLogMessage 是 Redis 传输中包装 LogMessage 的结构体，包含源实例 ID 用于去重。
type redisLogMessage struct {
	// Source 是发送消息的服务器实例 UUID，接收端用于跳过自我投递的消息。
	Source string     `json:"source"`
	// Msg 是实际的日志消息内容。
	Msg LogMessage `json:"msg"`
}

// PublishLog 对消息内容进行脱敏处理，投递给本地订阅者，并发布到 Redis 供其他服务器实例投递。
//
// 处理流程：
//  1. 对消息内容进行脱敏处理（API Key、Token、密码等）
//  2. 设置时间戳和任务 ID
//  3. 投递给本地订阅者（非阻塞，通道满时丢弃消息）
//  4. 缓冲到 Redis 有序集合（用于历史查询）
//  5. 发布到 Redis Pub/Sub 频道（供其他实例投递）
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 日志所属任务的 ID
//   - msg: 日志消息（Content 会被脱敏处理）
//
// 返回：
//   - error: Redis 发布失败时返回错误
func (g *Gateway) PublishLog(ctx context.Context, taskID string, msg LogMessage) error {
	// 发布前对内容进行脱敏处理
	msg.Content = Desensitize(msg.Content)

	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}
	msg.TaskID = taskID

	// 投递给本地客户端
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

	// 发布到 Redis 供跨实例投递（包含源实例 ID 用于去重）
	if g.redis == nil {
		return nil
	}
	wrapped := redisLogMessage{Source: g.id, Msg: msg}
	data, err := json.Marshal(wrapped)
	if err != nil {
		return fmt.Errorf("marshal log message: %w", err)
	}

	// 将消息缓冲到 Redis 有序集合中，用于历史查询
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

// Start 开始监听 Redis Pub/Sub 的 "logs:*" 模式消息，并将日志分发给本地订阅者。
// 阻塞直到 ctx 被取消。来自本实例的消息会被跳过以避免重复投递。
//
// 工作流程：
//  1. 使用 PSubscribe 订阅所有 "logs:*" 频道
//  2. 接收消息后反序列化为 redisLogMessage
//  3. 跳过来自本实例的消息（通过 Source 字段匹配）
//  4. 从频道名提取 taskID，分发给对应的本地订阅者
//
// 参数：
//   - ctx: 上下文，取消后停止监听
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

			// 跳过本实例的消息 — 已在本地投递
			if wrapped.Source == g.id {
				continue
			}

			// 从频道名 "logs:{taskID}" 中提取 taskID
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

// GetBufferedLogs 从 Redis 中获取指定任务的所有缓冲日志消息，按时间戳升序返回。
// 用于客户端重连后恢复历史日志。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务的唯一标识符
//
// 返回：
//   - []LogMessage: 缓冲的日志消息列表（按时间戳升序）
//   - error: Redis 查询失败时返回错误
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

// ClientCount 返回指定任务的订阅者数量。
//
// 参数：
//   - taskID: 任务的唯一标识符
//
// 返回：
//   - int: 当前订阅者数量
func (g *Gateway) ClientCount(taskID string) int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.clients[taskID])
}

// Close 关闭所有客户端通道，清理资源。在服务器关闭时调用。
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
