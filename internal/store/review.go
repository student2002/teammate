// review.go provides data access operations related to code review.
//
// This file includes:
//   - GetReviewQueue: get the review queue for the specified project (review nodes in pending and in_progress status)
//   - GetReviewNodeReviewer: get the reviewer of a review node (assignee_id)
//   - GetReviewNodeAuthor: get the author of the preceding node of a review node (assignee_id)
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// GetReviewQueue gets the review queue for the specified project, returning all review nodes in pending and in_progress status.
// Ordered by creation time ascending, with the earliest review first.
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//
// Returns:
//   - []types.GetReviewQueueRow: review queue list
//   - error: possible error (database query failure)
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

// GetReviewNodeReviewer gets the reviewer of a review node (assignee_id).
//
// Parameters:
//   - ctx: request context
//   - nodeID: review node ID
//   - taskID: task ID
//
// Returns:
//   - uuid.NullUUID: the reviewer's assignee_id (may be empty)
//   - error: possible error (node does not exist)
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

// GetReviewNodeAuthor gets the author of the preceding node of a review node (assignee_id).
// It finds the preceding node via the nearest node sorted before it (compatible with 0- or 1-based numbering).
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//   - nodeID: review node ID
//
// Returns:
//   - uuid.NullUUID: the preceding node author's assignee_id (may be empty)
//   - error: possible error (preceding node does not exist)
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
