// token_usage.go 提供 Token 用量统计的业务逻辑。
// 记录每次执行的 Token 消耗，用于成本分析和预算控制。
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// TokenUsageService 提供 Token 用量统计相关的业务逻辑。
type TokenUsageService struct {
	svc *Service
}

// NewTokenUsageService 创建一个新的 TokenUsageService 实例。
func NewTokenUsageService(svc *Service) *TokenUsageService {
	return &TokenUsageService{svc: svc}
}

// Create 记录一次 Token 用量。
func (s *TokenUsageService) Create(ctx context.Context, params types.CreateTokenUsageParams) (types.TokenUsage, error) {
	return s.svc.Store.CreateTokenUsage(ctx, params)
}

// GetByTask 获取指定任务的 Token 用量汇总。
func (s *TokenUsageService) GetByTask(ctx context.Context, taskID int32) (types.GetTokenUsageByTaskRow, error) {
	return s.svc.Store.GetTokenUsageByTask(ctx, taskID)
}

// GetByAgent 获取单个 Agent 的 Token 用量汇总。
func (s *TokenUsageService) GetByAgent(ctx context.Context, agentID uuid.UUID) (types.GetTokenUsageByAgentRow, error) {
	return s.svc.Store.GetTokenUsageByAgent(ctx, agentID)
}

// GetByAgents 批量获取多个 Agent 的 Token 用量汇总。
func (s *TokenUsageService) GetByAgents(ctx context.Context, agentIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByAgentsRow, error) {
	return s.svc.Store.GetTokenUsageByAgents(ctx, agentIDs)
}

// GetByTaskNodes 批量获取多个节点的 Token 用量汇总。
func (s *TokenUsageService) GetByTaskNodes(ctx context.Context, nodeIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByTaskNodesRow, error) {
	return s.svc.Store.GetTokenUsageByTaskNodes(ctx, nodeIDs)
}

// 静默 store 引用避免未导入警告（GetByAgent 等已透传 store 返回值，但本文件可能孤立）
var _ = store.Store{}
