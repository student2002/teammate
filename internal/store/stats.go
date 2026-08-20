// stats.go 提供项目和 Agent 的统计数据查询操作。
//
// 统计数据用于仪表盘展示，包括任务计数、节点完成率、
// 平均完成时间、Token 用量等指标。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ProjectStats 封装项目的统计信息。
//
// 包含任务按状态分组计数、节点完成率、平均完成时间。
type ProjectStats struct {
	TaskCounts         TaskCountsByStatus `json:"task_counts"`                // 按状态分组的任务计数
	NodeCompletionRate float64            `json:"node_completion_rate"`       // 节点完成率（0-1）
	AvgTimeToComplete  *float64           `json:"avg_time_to_complete_hours"` // 平均完成时间（小时）
}

// TaskCountsByStatus 按状态分组的任务计数。
type TaskCountsByStatus struct {
	Active    int64 `json:"active"`    // 活跃任务数
	Completed int64 `json:"completed"` // 已完成任务数
	Cancelled int64 `json:"cancelled"` // 已取消任务数
}

// AgentStats 封装 Agent 的统计信息。
//
// 包含完成任务数、Token 用量（输入/输出）、平均完成时间。
type AgentStats struct {
	TotalCompletedTasks int32     `json:"total_completed_tasks"`     // 总完成任务数
	TotalTokens         int64     `json:"total_tokens"`              // 总 Token 用量
	InputTokens         int64     `json:"input_tokens"`              // 输入 Token 用量
	OutputTokens        int64     `json:"output_tokens"`             // 输出 Token 用量
	AvgCompletionTime   *float64  `json:"avg_completion_time_hours"` // 平均完成时间（小时）
	ComputedAt          time.Time `json:"computed_at"`               // 统计计算时间
}

// GetProjectStats 查询项目的统计数据（任务计数、节点完成率、平均完成时间）。
//
// 执行三步查询：
//  1. 按状态分组统计任务数量
//  2. 计算节点完成率（已完成节点数 / 总节点数）
//  3. 计算已完成任务的平均完成时间
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//
// 返回：
//   - *ProjectStats: 项目统计数据
//   - error: 查询失败时返回错误
func (s *Store) GetProjectStats(ctx context.Context, projectID uuid.UUID) (*ProjectStats, error) {
	var counts TaskCountsByStatus
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'active') AS active,
			COUNT(*) FILTER (WHERE status = 'completed') AS completed,
			COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled
		FROM tasks
		WHERE project_id = $1
	`, projectID).Scan(&counts.Active, &counts.Completed, &counts.Cancelled)
	if err != nil {
		return nil, fmt.Errorf("query task counts: %w", err)
	}

	var nodeCompletionRate float64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			CASE WHEN COUNT(*) = 0 THEN 0
			ELSE (COUNT(*) FILTER (WHERE status = 'completed'))::float / COUNT(*)::float
			END
		FROM task_nodes
		WHERE task_id IN (SELECT id FROM tasks WHERE project_id = $1)
	`, projectID).Scan(&nodeCompletionRate)
	if err != nil {
		return nil, fmt.Errorf("query node completion rate: %w", err)
	}

	var avgTime sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			EXTRACT(EPOCH FROM AVG(updated_at - created_at)) / 3600.0
		FROM tasks
		WHERE project_id = $1
		  AND status = 'completed'
	`, projectID).Scan(&avgTime)
	if err != nil {
		return nil, fmt.Errorf("query avg time: %w", err)
	}

	var avgTimePtr *float64
	if avgTime.Valid {
		avgTimePtr = &avgTime.Float64
	}

	return &ProjectStats{
		TaskCounts:         counts,
		NodeCompletionRate: nodeCompletionRate,
		AvgTimeToComplete:  avgTimePtr,
	}, nil
}

// GetAgentStats 查询 Agent 的统计数据（完成任务数、Token 用量、平均完成时间）。
//
// 执行两步查询：
//  1. 从 token_usage 表实时聚合 Token 用量，从 task_nodes 表统计完成任务数
//  2. 关联 task_nodes 和 tasks 表计算平均完成时间
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - *AgentStats: Agent 统计数据
//   - error: 查询失败时返回错误
func (s *Store) GetAgentStats(ctx context.Context, agentID uuid.UUID) (*AgentStats, error) {
	var totalCompleted int32
	var inputTokens, outputTokens, totalTokens int64
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT COUNT(*) FROM task_nodes WHERE assignee_id = $1 AND status = 'completed'), 0),
			COALESCE(SUM(tu.input_tokens), 0),
			COALESCE(SUM(tu.output_tokens), 0),
			COALESCE(SUM(tu.total_tokens), 0)
		FROM token_usage tu
		WHERE tu.agent_id = $1
	`, agentID).Scan(&totalCompleted, &inputTokens, &outputTokens, &totalTokens)
	if err != nil {
		return nil, fmt.Errorf("query agent stats: %w", err)
	}

	var avgTime sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT
			EXTRACT(EPOCH FROM AVG(t.updated_at - t.created_at)) / 3600.0
		FROM tasks t
		JOIN task_nodes tn ON tn.task_id = t.id
		WHERE tn.assignee_id = $1
		  AND t.status = 'completed'
	`, agentID).Scan(&avgTime)
	if err != nil {
		return nil, fmt.Errorf("query agent avg time: %w", err)
	}

	var avgTimePtr *float64
	if avgTime.Valid {
		avgTimePtr = &avgTime.Float64
	}

	return &AgentStats{
		TotalCompletedTasks: totalCompleted,
		TotalTokens:         totalTokens,
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		AvgCompletionTime:   avgTimePtr,
		ComputedAt:          time.Now(),
	}, nil
}
