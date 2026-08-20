// review.go 提供代码审查相关的数据访问操作。
//
// 本文件包含：
//   - GetReviewQueue：获取指定项目的审查队列（pending 和 in_progress 状态的审查节点）
//   - GetReviewNodeReviewer：获取审查节点的审查者（assignee_id）
//   - GetReviewNodeAuthor：获取审查节点前序节点的作者（assignee_id）
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// GetReviewQueue 获取指定项目的审查队列，返回所有 pending 和 in_progress 状态的审查节点。
// 按创建时间正序排列，最早的审查排在前面。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - []types.GetReviewQueueRow: 审查队列列表
//   - error: 可能的错误（数据库查询失败）
func (s *Store) GetReviewQueue(ctx context.Context, projectID uuid.UUID) ([]types.GetReviewQueueRow, error) {
	rows, err := s.q.GetReviewQueue(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get review queue: %w", err)
	}
	out := make([]types.GetReviewQueueRow, 0, len(rows))
	for _, r := range rows {
		var assigneeID *string
		if r.AssigneeID.Valid {
			s := r.AssigneeID.UUID.String()
			assigneeID = &s
		}
		var agentName *string
		if r.AgentName.Valid {
			s := r.AgentName.String
			agentName = &s
		}
		out = append(out, types.GetReviewQueueRow{
			TaskID:       r.TaskID,
			TaskTitle:    r.TaskTitle,
			NodeID:       r.NodeID.String(),
			NodeName:     r.NodeName,
			NodeStatus:   string(r.NodeStatus),
			AssigneeType: string(r.AssigneeType),
			AssigneeID:   assigneeID,
			AgentName:    agentName,
			CreatedAt:    r.CreatedAt,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return out, nil
}

// GetReviewNodeReviewer 获取审查节点的审查者（assignee_id）。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 审查节点 ID
//   - taskID: 任务 ID
//
// 返回：
//   - uuid.NullUUID: 审查者的 assignee_id（可能为空）
//   - error: 可能的错误（节点不存在）
func (s *Store) GetReviewNodeReviewer(ctx context.Context, nodeID uuid.UUID, taskID int32) (uuid.NullUUID, error) {
	reviewerID, err := s.q.GetReviewNodeReviewer(ctx, db.GetReviewNodeReviewerParams{
		ID:     nodeID,
		TaskID: taskID,
	})
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("get review node reviewer: %w", err)
	}
	return reviewerID, nil
}

// GetReviewNodeAuthor 获取审查节点前序节点的作者（assignee_id）。
// 通过排序在前的最近节点查找前序节点（兼容 0 或 1 起始编号）。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//   - nodeID: 审查节点 ID
//
// 返回：
//   - uuid.NullUUID: 前序节点作者的 assignee_id（可能为空）
//   - error: 可能的错误（前序节点不存在）
func (s *Store) GetReviewNodeAuthor(ctx context.Context, taskID int32, nodeID uuid.UUID) (uuid.NullUUID, error) {
	authorID, err := s.q.GetReviewNodeAuthor(ctx, db.GetReviewNodeAuthorParams{
		TaskID: taskID,
		ID:     nodeID,
	})
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("get review node author: %w", err)
	}
	return authorID, nil
}
