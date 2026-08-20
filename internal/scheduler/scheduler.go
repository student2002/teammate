// scheduler.go 提供服务器端定时任务调度器，负责各类周期性维护任务的自动执行。
// 调度器使用 Redis 分布式锁确保多实例部署时任务不重复执行。
//
// 包含的定时任务：
//   - 心跳超时检测（15秒）：标记离线运行时，更新代理状态
//   - 续接预留清理（30秒）：清除过期的节点续接预留
//   - 认领超时释放（60分钟）：释放长时间无进展的 in_progress 节点
//   - 节点超时处理（5分钟）：处理超出模板超时时间的节点
//   - 离线回退（5分钟）：将离线代理持有的节点转为人工干预
//   - 代理自动恢复（60秒）：将无任务的 busy 代理恢复为 online
//   - 待认领节点重通知（30秒）：重新通知长时间未认领的 pending 节点
//   - 低置信度记忆清理（每日3点）：删除超过30天的低置信度记忆
//   - 旧工作区清理（每日4点）：识别超过7天的已完成任务供后续清理
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/clock"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// EventPublisher 定义调度器所需的最小事件发布接口。
type EventPublisher interface {
	Publish(ctx context.Context, subscriberID string, event types.SSEEvent) error
	BufferEvent(ctx context.Context, subscriberID string, event types.SSEEvent) error
}

// Scheduler 是服务器端定时任务调度器，负责管理各类周期性维护任务。
// 使用 Redis 分布式锁确保多实例部署时同一任务不会被重复执行。
type Scheduler struct {
	Store *store.Store
	Hub   EventPublisher
	Redis *redis.Client
	Clock clock.Clock
}

// NewScheduler 创建一个新的调度器实例。
//
// 参数：
//   - q: sqlc 生成的数据库查询实例
//   - db: 数据库连接池，用于需要事务的操作
//   - hub: SSE 事件发布器（ws.Hub 满足此接口）
//   - rdb: Redis 客户端，用于分布式锁
//
// 返回：
//   - *Scheduler: 初始化后的调度器实例，默认使用系统真实时间
func NewScheduler(st *store.Store, hub EventPublisher, rdb *redis.Client) *Scheduler {
	return &Scheduler{Store: st, Hub: hub, Redis: rdb, Clock: clock.RealClock{}}
}

// Start 启动所有定时任务并阻塞直到上下文取消。
// 启动后会创建多个 goroutine 分别运行不同周期的任务。
func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("scheduler started")

	go s.runPeriodic(ctx, 15*time.Second, "heartbeat_timeout", s.CheckHeartbeatTimeout)
	go s.runPeriodic(ctx, 30*time.Second, "reservation_cleanup", s.ClearExpiredReservations)
	go s.runPeriodic(ctx, 60*time.Second, "claim_timeout", s.ReleaseClaimTimeoutNodes)
	go s.runPeriodic(ctx, 300*time.Second, "node_timeout", s.CheckNodeTimeout)
	go s.runPeriodic(ctx, 300*time.Second, "offline_fallback", s.OfflineAgentFallback)
	go s.runPeriodic(ctx, 60*time.Second, "agent_auto_recover", s.AgentAutoRecoverOnline)
	go s.runPeriodic(ctx, 30*time.Second, "pending_node_renotify", s.RenotifyPendingNodes)
	go s.runPeriodic(ctx, 30*time.Second, "workflow_triggers", s.ProcessWorkflowTriggers)
	go s.runDaily(ctx, 3, 0, "memory_gc", s.lowConfidenceMemoryGC)
	go s.runDaily(ctx, 4, 0, "workspace_cleanup", s.CleanupOldWorkspaces)

	<-ctx.Done()
	slog.Info("scheduler stopped")
}

// ---------------------------------------------------------------------------
// 调度原语
// ---------------------------------------------------------------------------

// runPeriodic 按固定间隔周期性执行任务，使用 Redis 分布式锁防止多实例并发。
//
// 参数：
//   - ctx: 上下文，用于取消任务
//   - interval: 执行间隔
//   - name: 任务名称，用于 Redis 锁键
//   - fn: 要执行的任务函数
func (s *Scheduler) runPeriodic(ctx context.Context, interval time.Duration, name string, fn func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.WithLock(ctx, name, interval, fn)
		}
	}
}

// runDaily 在每天指定时间执行任务，适用于低频维护任务。
//
// 参数：
//   - ctx: 上下文，用于取消任务
//   - hour: 执行时间的小时（0-23）
//   - minute: 执行时间的分钟（0-59）
//   - name: 任务名称，用于 Redis 锁键
//   - fn: 要执行的任务函数
func (s *Scheduler) runDaily(ctx context.Context, hour, minute int, name string, fn func(context.Context) error) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}
		timer := time.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.WithLock(ctx, name, 24*time.Hour, fn)
		}
	}
}

// WithLock 使用 Redis 分布式锁执行任务，防止多实例并发执行同一任务。
// 锁的 TTL 略大于任务间隔（+5秒），确保任务完成前锁不会过期。
//
// 参数：
//   - ctx: 上下文
//   - name: 任务名称，作为 Redis 锁键的后缀
//   - lockTTL: 锁的过期时间
//   - fn: 要执行的任务函数
func (s *Scheduler) WithLock(ctx context.Context, name string, lockTTL time.Duration, fn func(context.Context) error) {
	lockKey := fmt.Sprintf("scheduler:lock:%s", name)
	// 使用 SETNX 获取锁，TTL 设为任务间隔+5秒，避免锁过期导致重复执行
	ok, err := s.Redis.SetNX(ctx, lockKey, "locked", lockTTL+5*time.Second).Result()
	if err != nil {
		slog.Error("redis setnx error", "task", name, "err", err)
		return
	}
	if !ok {
		return // 其他实例已持有锁
	}
	defer s.Redis.Del(ctx, lockKey)

	if err := fn(ctx); err != nil {
		slog.Error("scheduler task error", "task", name, "err", err)
	}
}

// ---------------------------------------------------------------------------
// 任务实现
// ---------------------------------------------------------------------------

// CheckHeartbeatTimeout 将过期的运行时标记为离线，并更新所有运行时离线的代理状态。
// 每 15 秒执行一次，检测超过 100 秒未发送心跳的运行时。
func (s *Scheduler) CheckHeartbeatTimeout(ctx context.Context) error {
	staleRuntimes, err := s.Store.MarkStaleRuntimes(ctx, s.Clock.Now().Add(-100*time.Second))
	if err != nil {
		slog.Error("mark stale runtimes", "err", err)
	} else if len(staleRuntimes) > 0 {
		slog.Info("marked stale runtimes offline", "count", len(staleRuntimes))
	}

	offlineAgents, err := s.Store.UpdateOfflineAgents(ctx)
	if err != nil {
		slog.Error("update offline agents", "err", err)
	} else if len(offlineAgents) > 0 {
		slog.Info("updated agents to offline", "count", len(offlineAgents))
	}

	slog.Info("heartbeat_timeout complete",
		"stale_runtimes", len(staleRuntimes),
		"offline_agents", len(offlineAgents),
	)
	return nil
}

// ClearExpiredReservations 清除续接节点上过期的预留。
// 每 30 秒执行一次，释放超过 30 秒未认领的续接预留，允许其他代理认领。
func (s *Scheduler) ClearExpiredReservations(ctx context.Context) error {
	clearedReservations, err := s.Store.ClearExpiredReservations(ctx, s.Clock.Now().Add(-30*time.Second))
	if err != nil {
		slog.Error("clear expired reservations", "err", err)
		return nil
	}
	if len(clearedReservations) > 0 {
		slog.Info("cleared expired reservations", "count", len(clearedReservations))
	}
	return nil
}

// ReleaseClaimTimeoutNodes 释放认领超时的节点（in_progress 超过 30 分钟无进展），
// 将其设为 manual_intervention 并向原始代理发送中断事件。
// 每 60 秒执行一次。
func (s *Scheduler) ReleaseClaimTimeoutNodes(ctx context.Context) error {
	releasedNodes, err := s.Store.ReleaseClaimTimeoutNodes(ctx, s.Clock.Now().Add(-30*time.Minute))
	if err != nil {
		slog.Error("release claim timeout nodes", "err", err)
		return nil
	}
	if len(releasedNodes) > 0 {
		slog.Info("claim timeout nodes detected", "count", len(releasedNodes))
		for _, node := range releasedNodes {
			// 向原始代理发送中断事件（SQL 保留了 assignee_id）
			if node.AssigneeID != nil {
				assigneeUUID, _ := uuid.Parse(*node.AssigneeID)
				nodeUUID, _ := uuid.Parse(node.ID)
				s.publishNodeTimeout(ctx, nodeUUID, assigneeUUID)
			}

			// 创建状态转换记录
			_, _ = s.Store.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
				TaskNodeID:   node.ID,
				FromStatus:   types.TaskNodeStatusInProgress,
				ToStatus:     types.TaskNodeStatusManualIntervention,
				Action:       types.TransitionActionTimeout,
				OperatorType: "system",
			})
		}
	}
	return nil
}

// CheckNodeTimeout 处理超出模板 timeout_minutes 的节点，将其设为 manual_intervention。
// 每 5 分钟执行一次，每个节点使用事务确保状态变更和过渡记录的原子性。
func (s *Scheduler) CheckNodeTimeout(ctx context.Context) error {
	timedOutNodes, err := s.Store.GetTimedOutNodes(ctx, s.Clock.Now())
	if err != nil {
		return err
	}

	if len(timedOutNodes) > 0 {
		slog.Info("found timed-out nodes", "count", len(timedOutNodes))
	}

	for _, node := range timedOutNodes {
		if err := s.Store.MoveTimedOutNodeToManual(ctx, node); err != nil {
			slog.Error("move timed-out node to manual_intervention", "node_id", node.ID, "err", err)
			continue
		}
		if node.AssigneeID != nil {
			assigneeUUID, _ := uuid.Parse(*node.AssigneeID)
			nodeUUID, _ := uuid.Parse(node.ID)
			s.publishNodeTimeout(ctx, nodeUUID, assigneeUUID)
		}
	}

	slog.Info("node_timeout complete", "timed_out_nodes", len(timedOutNodes))
	return nil
}

// OfflineAgentFallback 将离线超过 1 小时的代理所持有的 in_progress 节点
// 移至 manual_intervention，确保离线代理不会永久阻塞任务流转。
// 每 5 分钟执行一次。
func (s *Scheduler) OfflineAgentFallback(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-1 * time.Hour)
	affectedNodes, err := s.Store.OfflineAgentFallback(ctx, cutoff)
	if err != nil {
		slog.Error("offline agent fallback", "err", err)
		return nil
	}
	if len(affectedNodes) > 0 {
		slog.Info("offline agent fallback moved nodes to manual_intervention", "count", len(affectedNodes))
	}
	return nil
}

// AgentAutoRecoverOnline 将没有 in_progress 节点且状态为 "busy" 的代理
// 恢复为 "online"，防止代理在所有节点完成后卡在 "busy" 状态。
// 每 60 秒执行一次。
func (s *Scheduler) AgentAutoRecoverOnline(ctx context.Context) error {
	n, err := s.Store.AutoRecoverIdleAgents(ctx)
	if err != nil {
		slog.Error("agent auto recover online", "err", err)
		return nil
	}
	if n > 0 {
		slog.Info("auto recovered agents to online", "count", n)
	}
	return nil
}

// lowConfidenceMemoryGC 删除置信度低于 0.1、未验证且超过 30 天的记忆。
// 每日凌晨 3 点执行，清理低质量记忆以节省存储空间。
func (s *Scheduler) lowConfidenceMemoryGC(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-30 * 24 * time.Hour)
	n, err := s.Store.DeleteLowConfidenceMemories(ctx, cutoff)
	if err != nil {
		slog.Error("low-confidence memory GC", "err", err)
		return nil
	}
	if n > 0 {
		slog.Info("low-confidence memory GC deleted memories", "count", n)
	}
	return nil
}

// RenotifyPendingNodes 扫描长时间未认领的 pending 节点，重新向其项目成员 Agent 发布 node:pending 事件。
// 每 30 秒执行一次，补偿因运行时不在线而丢失的 SSE 事件。
// 按项目粒度投递：只通知项目成员 Agent（与 service.publishToProject 语义一致）。
func (s *Scheduler) RenotifyPendingNodes(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-30 * time.Second)
	projectIDs, err := s.Store.ListPendingRenotifyProjectIDs(ctx, cutoff)
	if err != nil {
		slog.Error("renotify pending nodes query", "err", err)
		return nil
	}

	for _, projectID := range projectIDs {
		data, _ := json.Marshal(map[string]string{
			"reason": "renotify_pending",
		})
		event := types.SSEEvent{
			ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
			Event: types.EventNodePending,
			Data:  data,
		}

		members, err := s.Store.ListProjectMembers(ctx, projectID)
		if err != nil {
			slog.Error("renotify list project members", "project_id", projectID, "err", err)
			continue
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
				slog.Error("renotify list runtimes", "agent_id", m.AgentID, "err", err)
				continue
			}
			for _, runtimeID := range runtimeIDs {
				if err := s.Hub.Publish(ctx, runtimeID.String(), event); err != nil {
					slog.Error("renotify publish", "runtime_id", runtimeID, "err", err)
					continue
				}
				onlineSent = true
			}
		}

		// 无在线成员运行时：缓冲到成员 Agent 的所有运行时（含离线），恢复后回放
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
				for _, runtimeID := range runtimeIDs {
					if err := s.Hub.BufferEvent(ctx, runtimeID.String(), event); err != nil {
						slog.Error("renotify buffer", "runtime_id", runtimeID, "err", err)
					}
				}
			}
		}
	}

	if len(projectIDs) > 0 {
		slog.Info("renotify pending nodes", "projects", len(projectIDs))
	}
	return nil
}

// CleanupOldWorkspaces 识别超过 7 天的已完成/已取消任务，
// 记录为清理候选。实际文件清理由 CLI 命令或代理接收清理事件执行。
// 每日凌晨 4 点执行，分批查询（每批 500 条）避免内存占用过高。
func (s *Scheduler) ProcessWorkflowTriggers(ctx context.Context) error {
	svc := &service.Service{
		Store: s.Store,
		Hub:   s.Hub,
		Redis: s.Redis,
	}
	return service.NewWorkflowTriggerService(svc).ProcessDueSchedules(ctx, s.Clock.Now())
}

func (s *Scheduler) CleanupOldWorkspaces(ctx context.Context) error {
	cutoff := s.Clock.Now().Add(-7 * 24 * time.Hour)
	const batchSize int32 = 500

	var allCandidates []types.GetCompletedTasksOlderThanRow
	var lastID int32

	for {
		rows, err := s.Store.GetCompletedTasksOlderThan(ctx, cutoff, lastID, batchSize)
		if err != nil {
			slog.Error("workspace cleanup query", "err", err)
			return nil
		}
		if len(rows) == 0 {
			break
		}
		allCandidates = append(allCandidates, rows...)
		lastID = rows[len(rows)-1].ID
		if int32(len(rows)) < batchSize {
			break
		}
	}

	if len(allCandidates) == 0 {
		slog.Info("workspace_cleanup complete, no candidates found")
		return nil
	}

	// 按工作区分组记录日志
	byWorkspace := make(map[uuid.UUID][]types.GetCompletedTasksOlderThanRow)
	for _, r := range allCandidates {
		wsUUID, _ := uuid.Parse(r.WorkspaceID)
		byWorkspace[wsUUID] = append(byWorkspace[wsUUID], r)
	}

	for wsID, tasks := range byWorkspace {
		taskIDs := make([]int32, 0, len(tasks))
		for _, t := range tasks {
			taskIDs = append(taskIDs, t.ID)
		}
		slog.Info("workspace_cleanup candidates",
			"workspace_id", wsID,
			"count", len(taskIDs),
			"task_ids", taskIDs,
		)
	}

	slog.Info("workspace_cleanup complete",
		"total_candidates", len(allCandidates),
		"workspaces", len(byWorkspace),
		"cutoff", cutoff.Format(time.RFC3339),
	)
	return nil
}

// ---------------------------------------------------------------------------
// 辅助函数
// ---------------------------------------------------------------------------

// publishNodeTimeout 向指定代理发送节点超时的 SSE 事件。
//
// 参数：
//   - ctx: 上下文
//   - nodeID: 超时的节点 ID
//   - agentID: 节点代理的 ID
func (s *Scheduler) publishNodeTimeout(ctx context.Context, nodeID, agentID uuid.UUID) {
	rt, err := s.Store.GetRuntimeByAgent(ctx, agentID)
	if err != nil {
		slog.Error("get runtime for agent", "agent_id", agentID, "err", err)
		return
	}

	runtimeID := rt.ID
	data, _ := json.Marshal(map[string]string{
		"node_id":  nodeID.String(),
		"action":   "timeout",
		"status":   "manual_intervention",
		"agent_id": agentID.String(),
	})

	if err := s.Hub.Publish(ctx, runtimeID, types.SSEEvent{
		ID:    fmt.Sprintf("%d", time.Now().UnixNano()),
		Event: types.EventNodeTimeout,
		Data:  data,
	}); err != nil {
		slog.Error("publish SSE node timeout", "runtime_id", runtimeID, "err", err)
	}
}
