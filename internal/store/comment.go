// comment.go 提供任务评论的数据访问操作。
//
// 评论支持多种类型：文本评论、代码审查意见、建议和问题。
// 评论支持提及（mentions）功能，可通知其他成员或 Agent。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateComment 为任务创建一条评论记录。
func (s *Store) CreateComment(ctx context.Context, params types.CreateCommentParams) (types.Comment, error) {
	dbParams, err := FromDomainCreateCommentParams(params)
	if err != nil {
		return types.Comment{}, fmt.Errorf("convert create comment params: %w", err)
	}
	comment, err := s.q.CreateComment(ctx, dbParams)
	if err != nil {
		return types.Comment{}, fmt.Errorf("create comment: %w", err)
	}
	return ToDomainComment(comment)
}

// ListComments 查询指定任务的所有评论列表。
func (s *Store) ListComments(ctx context.Context, taskID int32) ([]types.Comment, error) {
	comments, err := s.q.ListComments(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return ToDomainCommentSlice(comments)
}

// ListTaskLevelComments 查询指定任务的任务级评论列表。
func (s *Store) ListTaskLevelComments(ctx context.Context, taskID int32) ([]types.Comment, error) {
	comments, err := s.q.ListTaskLevelComments(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task level comments: %w", err)
	}
	return ToDomainCommentSlice(comments)
}

// ListNodeComments 查询指定节点评论区的评论列表。
func (s *Store) ListNodeComments(ctx context.Context, taskID int32, nodeID uuid.UUID) ([]types.Comment, error) {
	comments, err := s.q.ListNodeComments(ctx, db.ListNodeCommentsParams{
		TaskID: taskID,
		NodeID: uuid.NullUUID{UUID: nodeID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list node comments: %w", err)
	}
	return ToDomainCommentSlice(comments)
}

// ListExecutionContextComments 查询执行当前节点时应注入的评论上下文。
func (s *Store) ListExecutionContextComments(ctx context.Context, taskID int32, nodeID uuid.UUID, mentionID uuid.UUID) ([]types.Comment, error) {
	comments, err := s.q.ListExecutionContextComments(ctx, db.ListExecutionContextCommentsParams{
		TaskID:    taskID,
		NodeID:    uuid.NullUUID{UUID: nodeID, Valid: true},
		MentionID: mentionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list execution context comments: %w", err)
	}
	return ToDomainCommentSlice(comments)
}

// GetComment 根据 ID 查询单条评论记录。
func (s *Store) GetComment(ctx context.Context, id uuid.UUID) (types.Comment, error) {
	comment, err := s.q.GetComment(ctx, id)
	if err != nil {
		return types.Comment{}, fmt.Errorf("get comment: %w", err)
	}
	return ToDomainComment(comment)
}

// UpdateComment 更新评论的内容和提及列表。
func (s *Store) UpdateComment(ctx context.Context, id uuid.UUID, content string, mentions []uuid.UUID) (types.Comment, error) {
	comment, err := s.q.UpdateComment(ctx, db.UpdateCommentParams{
		ID:       id,
		Content:  content,
		Mentions: mentions,
	})
	if err != nil {
		return types.Comment{}, fmt.Errorf("update comment: %w", err)
	}
	return ToDomainComment(comment)
}
