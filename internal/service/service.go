// service.go 实现业务逻辑层的核心文件，定义了 Service 结构体作为所有子服务的容器。
// 提供 SSE 事件的发布能力，支持向工作区广播、向特定代理发送。
// 以及发送控制事件（中断/回滚/续邀），控制事件会持久化到 Redis 缓冲，
// 确保 Agent 离线恢复后不丢失控制事件。
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

// EventPublisher 定义事件发布接口，用于向 Agent 运行时推送 SSE 事件。
// 由基础设施层实现。
type EventPublisher interface {
	Publish(ctx context.Context, subscriberID string, event types.SSEEvent) error
	BufferEvent(ctx context.Context, subscriberID string, event types.SSEEvent) error
}

// sseIDCounter 确保在高并发下 SSE 事件 ID 的唯一性。
// 使用原子递增计数器配合时间戳生成全局唯一 ID。
var sseIDCounter int64

// nextSSEEventID 生成唯一的 SSE 事件 ID，格式为 "{纳秒时间戳}-{递增计数器}"。
func nextSSEEventID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), atomic.AddInt64(&sseIDCounter, 1))
}

// Service 持有所有数据存储和基础设施连接，提供业务逻辑编排能力。
// 作为所有子服务（AgentService、TaskService、NodeService 等）的统一入口，
// 管理 Store、EventPublisher、Redis 和数据库连接。
type Service struct {
	Store *store.Store   // 数据访问层，封装 sqlc 生成的查询
	Hub   EventPublisher // SSE 事件发布器，管理 Agent 连接和事件广播
	Redis *redis.Client  // Redis 客户端，用于缓存、分布式锁、事件缓冲
}

// New 创建一个新的 Service 实例。
//
// 参数：
//   - pgDB: PostgreSQL 数据连接
//   - hub: SSE 事件发布器（可为 nil，此时 SSE 功能禁用，
//   - rdb: Redis 客户端（可为 nil，此时缓存和缓冲功能禁用，
//
// 返回：
//   - *Service: 初始化完成的 Service 实例
func New(pgDB *sql.DB, hub EventPublisher, rdb *redis.Client) *Service {
	return &Service{
		Store: store.New(pgDB),
		Hub:   hub,
		Redis: rdb,
	}
}

// NewWithClock 创建一个使用自定义时钟的 Service 实例（用于测试）。
// 自定义时钟允许在单元测试中控制时间流逝。
func NewWithClock(pgDB *sql.DB, hub EventPublisher, rdb *redis.Client, c clock.Clock) *Service {
	return &Service{
		Store: store.NewWithClock(pgDB, c),
		Hub:   hub,
		Redis: rdb,
	}
}

// publishToProject 向指定项目的 Agent 成员广播 SSE 事件（项目粒度投递）。
// 与工作区广播的区别：只投递给「该项目成员」Agent 的在线运行时——
// 非项目成员收不到节点通知（节点认领的消费者就是项目成员 Agent，见
// service/node.go Claim 的 CheckAgentProjectAccess），避免无关 Agent
// 感知到项目内任务的存在。
// 如果没有在线运行时，事件缓冲到成员 Agent 的所有运行时（包括离线的），
// 确保运行时重新上线时能收到丢失的事件（SSE 断线补偿机制）。
// 如果 Hub 为 nil（例如测试环境），则为空操作。
//
// 步骤：
//  1. 查询项目的 Agent 成员（project_members）
//  2. 将事件数据序列化为 JSON
//  3. 对每个成员 Agent 查询在线运行时并投递
//  4. 若无在线运行时，为成员 Agent 的所有运行时（包括离线的）缓冲事件用于恢复
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//   - eventType: SSE 事件类型（如 node:pending）
//   - data: 事件数据（将被 JSON 序列化）
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

	// 无在线成员运行时：为成员 Agent 的所有运行时（含离线）缓冲事件，
	// 离线恢复后通过 Last-Event-ID 回放
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

// publishToAgent 向指定的代理的所有在线运行时发送 SSE 事件。
// 如果 Hub 为 nil（例如测试环境）或无在线运行时，则为空操作。
//
// 步骤：
//  1. 查询代理的所有在线运行时 ID
//  2. 若无在线运行时，记录告警日志并丢弃事件
//  3. 将事件数据序列化为 JSON
//  4. 向所有在线运行时投递事件
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - eventType: SSE 事件类型
//   - data: 事件数据（将被 JSON 序列化）
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

// publishControlEvent 持久化控制事件并发布。
// 控制事件（中断/回滚/续邀）不能丢失，即使目标运行时离线也能恢复。
// 事件同时存储在 SSE 事件缓冲区（Redis）中，运行时重新上线时可获取。
//
// 步骤：
//  1. 向在线运行时发送事件（尽力投递）
//  2. 查询代理的所有运行时（包括离线的），
//  3. 为每个运行时缓冲事件到 Redis
//  4. 运行时重连时通过 Last-Event-ID 从 Redis 回放丢失事件
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - eventType: 控制事件类型（如 task:interrupt/node:reject_rollback/node:continuation_invite）
//   - data: 事件数据（将被 JSON 序列化）
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
