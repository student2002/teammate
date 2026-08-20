// token_usage.go 提供 Token 用量记录的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// toInt64 将 sqlc 生成的 interface{}（来自 COALESCE SUM）安全转换为 int64。
func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

// CreateTokenUsage 创建一条 Token 用量记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: Token 用量参数（含 task_node_id、agent_id、各类 token 数、费用估算）
//
// 返回：
//   - types.TokenUsage: 创建的用量记录
//   - error: 创建失败时返回错误
func (s *Store) CreateTokenUsage(ctx context.Context, params types.CreateTokenUsageParams) (types.TokenUsage, error) {
	dbParams, err := FromDomainCreateTokenUsageParams(params)
	if err != nil {
		return types.TokenUsage{}, fmt.Errorf("convert create token usage params: %w", err)
	}
	usage, err := s.q.CreateTokenUsage(ctx, dbParams)
	if err != nil {
		return types.TokenUsage{}, fmt.Errorf("create token usage: %w", err)
	}
	return ToDomainTokenUsage(usage)
}

// GetTokenUsageByTask 获取指定任务的 Token 用量汇总。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//
// 返回：
//   - types.GetTokenUsageByTaskRow: 用量汇总（input/output/total tokens + 费用估算）
//   - error: 查询失败时返回错误
func (s *Store) GetTokenUsageByTask(ctx context.Context, taskID int32) (types.GetTokenUsageByTaskRow, error) {
	usage, err := s.q.GetTokenUsageByTask(ctx, taskID)
	if err != nil {
		return types.GetTokenUsageByTaskRow{}, fmt.Errorf("get token usage by task: %w", err)
	}
	var cost *string
	if s, ok := usage.CostEstimate.(string); ok && s != "" {
		cost = &s
	}
	return types.GetTokenUsageByTaskRow{
		InputTokens:  toInt64(usage.InputTokens),
		OutputTokens: toInt64(usage.OutputTokens),
		TotalTokens:  toInt64(usage.TotalTokens),
		CostEstimate: cost,
	}, nil
}

// GetTokenUsageByAgent 获取单个 Agent 的 Token 用量汇总（从 token_usage 表实时聚合）。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent UUID
//
// 返回：
//   - types.GetTokenUsageByAgentRow: 用量汇总
//   - error: 查询失败时返回错误
func (s *Store) GetTokenUsageByAgent(ctx context.Context, agentID uuid.UUID) (types.GetTokenUsageByAgentRow, error) {
	row, err := s.q.GetTokenUsageByAgent(ctx, agentID)
	if err != nil {
		return types.GetTokenUsageByAgentRow{}, fmt.Errorf("get token usage by agent: %w", err)
	}
	return types.GetTokenUsageByAgentRow{
		InputTokens:  toInt64(row.InputTokens),
		OutputTokens: toInt64(row.OutputTokens),
		TotalTokens:  toInt64(row.TotalTokens),
	}, nil
}

// GetTokenUsageByAgents 批量获取多个 Agent 的 Token 用量汇总（一次查询，按 agent_id 分组）。
//
// 参数：
//   - ctx: 请求上下文
//   - agentIDs: Agent UUID 列表
//
// 返回：
//   - map[uuid.UUID]types.GetTokenUsageByAgentsRow: 按 Agent UUID 分组的用量汇总
//   - error: 查询失败时返回错误
func (s *Store) GetTokenUsageByAgents(ctx context.Context, agentIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByAgentsRow, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}
	rows, err := s.q.GetTokenUsageByAgents(ctx, agentIDs)
	if err != nil {
		return nil, fmt.Errorf("get token usage by agents: %w", err)
	}
	result := make(map[uuid.UUID]types.GetTokenUsageByAgentsRow, len(rows))
	for _, r := range rows {
		result[r.AgentID] = types.GetTokenUsageByAgentsRow{
			AgentID:      r.AgentID.String(),
			InputTokens:  toInt64(r.InputTokens),
			OutputTokens: toInt64(r.OutputTokens),
			TotalTokens:  toInt64(r.TotalTokens),
		}
	}
	return result, nil
}

// GetTokenUsageByTaskNodes 批量获取多个节点的 Token 用量汇总（一次查询，按 task_node_id 分组）。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeIDs: 节点 UUID 列表
//
// 返回：
//   - map[uuid.UUID]types.GetTokenUsageByTaskNodesRow: 按节点 UUID 分组的用量汇总
//   - error: 查询失败时返回错误
func (s *Store) GetTokenUsageByTaskNodes(ctx context.Context, nodeIDs []uuid.UUID) (map[uuid.UUID]types.GetTokenUsageByTaskNodesRow, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	rows, err := s.q.GetTokenUsageByTaskNodes(ctx, nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("get token usage by task nodes: %w", err)
	}
	result := make(map[uuid.UUID]types.GetTokenUsageByTaskNodesRow, len(rows))
	for _, r := range rows {
		result[r.TaskNodeID] = types.GetTokenUsageByTaskNodesRow{
			TaskNodeID:   r.TaskNodeID.String(),
			InputTokens:  toInt64(r.InputTokens),
			OutputTokens: toInt64(r.OutputTokens),
			TotalTokens:  toInt64(r.TotalTokens),
		}
	}
	return result, nil
}
