// runtime.go 实现运行时（Agentd 守护进程实例）的业务逻辑，
// 包括注册、心跳维护、状态列表和全量同步。
// 注册后通过 SSE 发送 sync:required 事件触发代理全量同步，
// 确保守护进程能发现注册前创建的待处理节点。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// RuntimeService 提供运行时管理相关的业务逻辑。
type RuntimeService struct {
	svc *Service
}

func NewRuntimeService(svc *Service) *RuntimeService {
	return &RuntimeService{svc: svc}
}

// Register 创建一个运行时并将代理状态更新为在线。
// 注册完成后通过 SSE 发送 sync:required 事件，使守护进程立即发现注册前创建的待处理节点。
func (s *RuntimeService) Register(ctx context.Context, params types.CreateRuntimeParams) (types.Runtime, error) {
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("parse agent id: %w", err)
	}
	if _, err := s.svc.Store.GetAgent(ctx, agentID); err != nil {
		return types.Runtime{}, fmt.Errorf("agent %s not found: %w", params.AgentID, err)
	}

	runtime, err := s.svc.Store.CreateRuntime(ctx, params)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("create runtime: %w", err)
	}

	// 注册 runtime 后将 Agent 状态置为 online
	if _, err := s.svc.Store.UpdateAgentStatus(ctx, types.UpdateAgentStatusParams{
		ID:     agentID.String(),
		Status: types.AgentStatusOnline,
	}); err != nil {
		_ = err
	}

	s.svc.publishToAgent(ctx, agentID, "sync:required", map[string]interface{}{
		"runtime_id": runtime.ID,
		"reason":     "new_registration",
	})

	return runtime, nil
}

// Heartbeat 更新运行时的心跳时间，维持 online 状态。
func (s *RuntimeService) Heartbeat(ctx context.Context, id uuid.UUID) (types.Runtime, error) {
	return s.svc.Store.UpdateRuntimeHeartbeat(ctx, id)
}

// List 列出所有运行时。
func (s *RuntimeService) List(ctx context.Context) ([]types.Runtime, error) {
	return s.svc.Store.ListRuntimes(ctx)
}

// ListByWorkspace 列出指定工作区内所有代理的运行时，使用 JOIN 查询避免 N+1 问题。
func (s *RuntimeService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]types.Runtime, error) {
	return s.svc.Store.ListRuntimesByWorkspace(ctx, workspaceID)
}

// GetRuntimeByID 根据 ID 获取运行时信息。
func (s *RuntimeService) GetRuntimeByID(ctx context.Context, runtimeID uuid.UUID) (types.Runtime, error) {
	runtime, err := s.svc.Store.GetRuntimeByID(ctx, runtimeID)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("get runtime by id: %w", err)
	}
	return runtime, nil
}

// Sync 为指定运行时执行全量状态同步，返回所有待处理节点、活跃任务和最近提及。
func (s *RuntimeService) Sync(ctx context.Context, runtimeID uuid.UUID) (*store.SyncResult, error) {
	return s.svc.Store.SyncRuntime(ctx, runtimeID)
}
