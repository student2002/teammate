// comment.go provides data access operations for task comments.
//
// Comments support multiple types: text comments, code review opinions, suggestions, and questions.
// Comments support the mentions feature, which can notify other members or Agents.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateComment creates a comment record for a task.
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

// ListComments queries all comments for the specified task.
func (s *Store) ListComments(ctx context.Context, taskID int32) ([]types.Comment, error) {
	comments, err := s.q.ListComments(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return ToDomainCommentSlice(comments)
}

// ListTaskLevelComments queries the task-level comments for the specified task.
func (s *Store) ListTaskLevelComments(ctx context.Context, taskID int32) ([]types.Comment, error) {
	comments, err := s.q.ListTaskLevelComments(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task level comments: %w", err)
	}
	return ToDomainCommentSlice(comments)
}

// ListNodeComments queries the comments in the specified node comment section.
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

// ListExecutionContextComments queries the comment context to inject when executing the current node.
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

// GetComment queries a single comment record by ID.
func (s *Store) GetComment(ctx context.Context, id uuid.UUID) (types.Comment, error) {
	comment, err := s.q.GetComment(ctx, id)
	if err != nil {
		return types.Comment{}, fmt.Errorf("get comment: %w", err)
	}
	return ToDomainComment(comment)
}

// UpdateComment updates the content and mentions list of a comment.
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
