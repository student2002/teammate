// stats.go 实现项目和代理的统计数据查询业务逻辑。
// 提供项目维度（任务总数、完成数、Token 消耗等）和代理维度（完成任务数、
// Token 消耗、成功率等）的聚合统计查询。
//
// 本文件包含：
//   - StatsService 结构体：提供统计查询相关的业务逻辑封装
//   - GetProjectStats：获取指定项目的聚合统计数据，包括任务计数、Token 消耗等
//   - GetAgentStats：获取指定代理的聚合统计数据，包括完成任务数、Token 消耗等
//
// 统计维度：
//   - 项目维度：任务总数、已完成数、进行中数、已拒绝数、Token 总消耗等
//   - 代理维度：已完成任务数、Token 总消耗、平均完成时间等
//
// 数据来源：统计数据通过 Store 层的 SQL 聚合查询获取，支持实时计算。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
)

// StatsService 提供统计查询相关的业务逻辑。
type StatsService struct {
	svc *Service
}

// NewStatsService 创建一个新的 StatsService 实例。
func NewStatsService(svc *Service) *StatsService {
	return &StatsService{svc: svc}
}

// GetProjectStats 获取指定项目的统计数据，包括任务总数、完成数、进行中数、
// 拒绝数、Token 总消耗等聚合指标。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - *store.ProjectStats: 项目统计数据（任务计数、Token 消耗等）
//   - error: 可能的错误（数据库查询失败）
func (s *StatsService) GetProjectStats(ctx context.Context, projectID uuid.UUID) (*store.ProjectStats, error) {
	stats, err := s.svc.Store.GetProjectStats(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project stats: %w", err)
	}
	return stats, nil
}

// GetAgentStats 获取指定代理的统计数据，包括已完成任务数、Token 总消耗、
// 平均完成时间等聚合指标。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//
// 返回：
//   - *store.AgentStats: 代理统计数据（任务计数、Token 消耗等）
//   - error: 可能的错误（数据库查询失败）
func (s *StatsService) GetAgentStats(ctx context.Context, agentID uuid.UUID) (*store.AgentStats, error) {
	stats, err := s.svc.Store.GetAgentStats(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent stats: %w", err)
	}
	return stats, nil
}
