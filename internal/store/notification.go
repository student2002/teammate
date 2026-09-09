// notification.go provides data access operations for notifications.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// ListManualInterventionNodes lists nodes in the workspace that require manual intervention.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - []types.ListManualInterventionNodesRow: list of nodes pending manual intervention (including task title)
//   - error: possible error (database query failure)
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

// ListMentionComments lists comments containing @mentions.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - memberID: ID of the mentioned member
//
// Returns:
//   - []types.ListMentionCommentsRow: list of mention comments (including task title)
//   - error: possible error (database query failure)
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
