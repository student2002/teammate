// notification.go 提供通知相关的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// ListManualInterventionNodes 列出工作区中需要人工介入的节点。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - []types.ListManualInterventionNodesRow: 待人工介入节点列表（含任务标题）
//   - error: 可能的错误（数据库查询失败）
func (s *Store) ListManualInterventionNodes(ctx context.Context, workspaceID uuid.UUID) ([]types.ListManualInterventionNodesRow, error) {
	rows, err := s.q.ListManualInterventionNodes(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list manual intervention nodes: %w", err)
	}
	out := make([]types.ListManualInterventionNodesRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, types.ListManualInterventionNodesRow{
			ID:        r.ID.String(),
			TaskID:    r.TaskID,
			Name:      r.Name,
			Status:    string(r.Status),
			CreatedAt: r.CreatedAt,
			TaskTitle: r.TaskTitle,
		})
	}
	return out, nil
}

// ListMentionComments 列出包含 @提及 的评论。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - memberID: 被提及的成员 ID
//
// 返回：
//   - []types.ListMentionCommentsRow: 提及评论列表（含任务标题）
//   - error: 可能的错误（数据库查询失败）
func (s *Store) ListMentionComments(ctx context.Context, workspaceID, memberID uuid.UUID) ([]types.ListMentionCommentsRow, error) {
	rows, err := s.q.ListMentionComments(ctx, db.ListMentionCommentsParams{
		WorkspaceID: workspaceID,
		Column2:     memberID,
	})
	if err != nil {
		return nil, fmt.Errorf("list mention comments: %w", err)
	}
	out := make([]types.ListMentionCommentsRow, 0, len(rows))
	for _, r := range rows {
		mentions := make([]string, 0, len(r.Mentions))
		for _, m := range r.Mentions {
			mentions = append(mentions, m.String())
		}
		out = append(out, types.ListMentionCommentsRow{
			ID:         r.ID.String(),
			TaskID:     r.TaskID,
			Content:    r.Content,
			Mentions:   mentions,
			CreatedAt:  r.CreatedAt,
			AuthorType: r.AuthorType,
			AuthorID:   r.AuthorID.String(),
			TaskTitle:  r.TaskTitle,
		})
	}
	return out, nil
}
